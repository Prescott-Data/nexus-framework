package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/discovery"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
)

// ConsentSpec represents the response for consent specification
type ConsentSpec struct {
	AuthURL    string   `json:"authUrl"`
	State      string   `json:"state"`
	Scopes     []string `json:"scopes"`
	ProviderID string   `json:"provider_id"`
}

// ConsentHandler handles OAuth consent flow
type ConsentHandler struct {
	db             *sqlx.DB
	baseURL        string
	redirectPath   string
	stateKey       []byte
	httpClient     *http.Client
	consentsMetric prometheus.Counter
	consentsOpenID prometheus.Counter
}

// ConsentHandlerConfig holds the dependencies for ConsentHandler
type ConsentHandlerConfig struct {
	DB           *sqlx.DB
	BaseURL      string
	RedirectPath string
	StateKey     []byte
	HTTPClient   *http.Client
}

// NewConsentHandler creates a new consent handler
func NewConsentHandler(cfg ConsentHandlerConfig) *ConsentHandler {
	metric := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_consents_created_total",
		Help: "Total OAuth consents created",
	})
	metricOpenID := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_consents_with_openid_total",
		Help: "Total OAuth consents where openid scope was requested",
	})

	collectors := []prometheus.Collector{metric, metricOpenID}
	for _, c := range collectors {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}

	return &ConsentHandler{
		db:             cfg.DB,
		baseURL:        cfg.BaseURL,
		redirectPath:   cfg.RedirectPath,
		stateKey:       cfg.StateKey,
		httpClient:     cfg.HTTPClient,
		consentsMetric: metric,
		consentsOpenID: metricOpenID,
	}
}

// GetSpec handles POST /auth/consent-spec
func (h *ConsentHandler) GetSpec(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WorkspaceID string   `json:"workspace_id"`
		ProviderID  string   `json:"provider_id"`
		Scopes      []string `json:"scopes"`
		ReturnURL   string   `json:"return_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	// Validate required fields
	if request.WorkspaceID == "" || request.ProviderID == "" || request.ReturnURL == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_fields", "Missing required fields")
		return
	}
	// Validate return URL domain if enforced
	if !server.IsReturnURLAllowed(request.ReturnURL) {
		httputil.WriteError(w, http.StatusBadRequest, "return_url_not_allowed", "return_url not allowed")
		return
	}

	// Get provider profile
	var provider struct {
		ID       uuid.UUID        `db:"id"`
		Name     string           `db:"name"`
		AuthType string           `db:"auth_type"`
		AuthURL  sql.NullString   `db:"auth_url"`
		ClientID sql.NullString   `db:"client_id"`
		Scopes   []string         `db:"scopes"`
		Params   *json.RawMessage `db:"params"`
	}

	err := h.db.QueryRow(
		"SELECT id, name, auth_type, auth_url, client_id, scopes, params FROM provider_profiles WHERE id = $1",
		request.ProviderID,
	).Scan(&provider.ID, &provider.Name, &provider.AuthType, &provider.AuthURL, &provider.ClientID, pq.Array(&provider.Scopes), &provider.Params)
	if err != nil {
		log.Printf("/auth/consent-spec provider lookup error: %v", err)
		httputil.WriteError(w, http.StatusNotFound, "provider_not_found", "Provider not found")
		return
	}

	switch provider.AuthType {
	case "oauth2", "":
		// Generate PKCE
		codeVerifier, codeChallenge, err := auth.GeneratePKCE()
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "pkce_failed", "Failed to generate PKCE")
			return
		}

		// Create connection record
		connectionID := uuid.New()
		expiresAt := time.Now().Add(10 * time.Minute)

		_, err = h.db.Exec(`
			INSERT INTO connections (id, workspace_id, provider_id, code_verifier, scopes, return_url, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			connectionID, request.WorkspaceID, request.ProviderID, codeVerifier, pq.Array(request.Scopes), request.ReturnURL, expiresAt)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "connection_create_failed", "Failed to create connection")
			return
		}

		// Generate signed state
		stateData := auth.StateData{
			WorkspaceID: request.WorkspaceID,
			ProviderID:  request.ProviderID,
			Nonce:       connectionID.String(),
			IAT:         time.Now(),
		}

		signedState, err := auth.SignState(h.stateKey, stateData)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "state_sign_failed", "Failed to sign state")
			return
		}

		// Attempt OIDC discovery to use the provider's authorization_endpoint
		// Only if 'openid' scope is requested to avoid overwriting standard OAuth2 endpoints (e.g. Slack)
		useAuthURL := provider.AuthURL.String
		hasOpenID := false
		for _, s := range request.Scopes {
			if strings.EqualFold(s, "openid") {
				hasOpenID = true
				break
			}
		}

		if hasOpenID && useAuthURL != "" {
			if md, errD := discovery.Discover(r.Context(), h.httpClient, discovery.Hint{AuthURL: useAuthURL}); errD == nil && strings.TrimSpace(md.AuthorizationEndpoint) != "" {
				useAuthURL = md.AuthorizationEndpoint
			}
		}

		// Build auth URL
		authURL, err := h.buildAuthURL(useAuthURL, provider.ClientID.String, signedState, codeChallenge, request.Scopes, provider.Params)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "auth_url_failed", "Failed to build auth URL")
			return
		}

		response := ConsentSpec{
			AuthURL:    authURL,
			State:      signedState,
			Scopes:     request.Scopes,
			ProviderID: request.ProviderID,
		}

		httputil.WriteJSON(w, http.StatusOK, response)
	case "api_key", "basic_auth":
		// Create Connection
		connectionID := uuid.New()
		expiresAt := time.Now().Add(10 * time.Minute)
		_, err = h.db.Exec(`
			INSERT INTO connections (id, workspace_id, provider_id, scopes, return_url, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			connectionID, request.WorkspaceID, request.ProviderID, pq.Array(request.Scopes), request.ReturnURL, expiresAt)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "connection_create_failed", "Failed to create connection")
			return
		}

		// Generate State
		stateData := auth.StateData{
			WorkspaceID: request.WorkspaceID,
			ProviderID:  request.ProviderID,
			Nonce:       connectionID.String(),
			IAT:         time.Now(),
		}
		signedState, err := auth.SignState(h.stateKey, stateData)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "state_sign_failed", "Failed to sign state")
			return
		}

		// Build Internal URL to the schema endpoint
		brokerBaseURL := strings.TrimSuffix(h.baseURL, "")
		capturePath := "/auth/capture-schema"

		u, _ := url.Parse(brokerBaseURL + capturePath)
		q := u.Query()
		q.Set("state", signedState)
		u.RawQuery = q.Encode()

		response := ConsentSpec{
			AuthURL:    u.String(),
			State:      signedState,
			Scopes:     request.Scopes,
			ProviderID: request.ProviderID,
		}

		httputil.WriteJSON(w, http.StatusOK, response)
	default:
		httputil.WriteError(w, http.StatusBadRequest, "unsupported_auth_type", "Unsupported provider auth_type")
		return
	}

	// increment metric after successful response
	h.consentsMetric.Inc()
	// increment when openid scope included
	for _, s := range request.Scopes {
		if strings.EqualFold(s, "openid") {
			h.consentsOpenID.Inc()
			break
		}
	}
}

// buildAuthURL constructs the OAuth authorization URL
func (h *ConsentHandler) buildAuthURL(providerAuthURL, clientID, state, codeChallenge string, scopes []string, providerParams *json.RawMessage) (string, error) {
	baseURL := strings.TrimSuffix(h.baseURL, "/")
	redirectPath := h.redirectPath

	if providerAuthURL == "" {
		return "", fmt.Errorf("provider auth_url is required for OAuth2")
	}

	u, err := url.Parse(providerAuthURL)
	if err != nil {
		return "", err
	}

	skipScopeOnAuth := false
	if providerParams != nil {
		var paramsMap map[string]interface{}
		if err := json.Unmarshal(*providerParams, &paramsMap); err == nil {
			if skip, ok := paramsMap["skip_scope_on_auth"].(bool); ok {
				skipScopeOnAuth = skip
			}
		}
	}

	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", baseURL+redirectPath)
	q.Set("response_type", "code")

	if !skipScopeOnAuth {
		if len(scopes) > 0 {
			q.Set("scope", strings.Join(scopes, " "))
		} else {
			// Backwards compatibility or provider defaults might expect an empty scope parameter,
			// but we only set it if not explicitly skipping.
			q.Set("scope", "")
		}
	}

	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")

	// When OIDC is requested, include a nonce to bind the ID token
	for _, s := range scopes {
		if strings.EqualFold(s, "openid") {
			// Use the connection ID embedded in the signed state as nonce
			// This will be verified against the id_token's nonce claim on callback
			// State format is base64(data).base64(hmac), where data contains Nonce
			// We can safely reuse the state value itself as nonce binding input without leaking secrets
			q.Set("nonce", state)
			break
		}
	}

	if providerParams != nil && len(*providerParams) > 0 {
		var params map[string]string
		if err := json.Unmarshal(*providerParams, &params); err == nil {
			for key, value := range params {
				q.Set(key, value)
			}
		}
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}
