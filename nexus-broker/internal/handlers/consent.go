package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"

	"dromos.com/nexus-broker/internal/auth"
	"dromos.com/nexus-broker/internal/discovery"
	"dromos.com/nexus-broker/internal/server"
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
	stateKey       []byte
	httpClient     *http.Client
	consentsMetric prometheus.Counter
	consentsOpenID prometheus.Counter
}

// NewConsentHandler creates a new consent handler
func NewConsentHandler(db *sqlx.DB, baseURL string, stateKey []byte, httpClient *http.Client) *ConsentHandler {
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
		db:             db,
		baseURL:        baseURL,
		stateKey:       stateKey,
		httpClient:     httpClient,
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
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.WorkspaceID == "" || request.ProviderID == "" || request.ReturnURL == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}
	// Validate return URL domain if enforced
	if !server.IsReturnURLAllowed(request.ReturnURL) {
		http.Error(w, "return_url not allowed", http.StatusBadRequest)
		return
	}

	// Get provider profile
	var provider struct {
		ID       uuid.UUID        `db:"id"`
		Name     string           `db:"name"`
		AuthType string           `db:"auth_type"`
		AuthURL  string           `db:"auth_url"`
		ClientID string           `db:"client_id"`
		Scopes   []string         `db:"scopes"`
		Params   *json.RawMessage `db:"params"`
	}

	err := h.db.QueryRow(
		"SELECT id, name, auth_type, auth_url, client_id, scopes, params FROM provider_profiles WHERE id = $1",
		request.ProviderID,
	).Scan(&provider.ID, &provider.Name, &provider.AuthType, &provider.AuthURL, &provider.ClientID, pq.Array(&provider.Scopes), &provider.Params)
	if err != nil {
		log.Printf("/auth/consent-spec provider lookup error: %v", err)
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	switch provider.AuthType {
	case "oauth2", "":
		// Generate PKCE
		codeVerifier, codeChallenge, err := auth.GeneratePKCE()
		if err != nil {
			http.Error(w, "Failed to generate PKCE", http.StatusInternalServerError)
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
			http.Error(w, "Failed to create connection", http.StatusInternalServerError)
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
			http.Error(w, "Failed to sign state", http.StatusInternalServerError)
			return
		}

		// Attempt OIDC discovery to use the provider's authorization_endpoint
		// Only if 'openid' scope is requested to avoid overwriting standard OAuth2 endpoints (e.g. Slack)
		useAuthURL := provider.AuthURL
		hasOpenID := false
		for _, s := range request.Scopes {
			if strings.EqualFold(s, "openid") {
				hasOpenID = true
				break
			}
		}

		if hasOpenID {
			if md, errD := discovery.Discover(r.Context(), h.httpClient, discovery.Hint{AuthURL: provider.AuthURL}); errD == nil && strings.TrimSpace(md.AuthorizationEndpoint) != "" {
				useAuthURL = md.AuthorizationEndpoint
			}
		}

		// Build auth URL
		authURL, err := h.buildAuthURL(useAuthURL, provider.ClientID, signedState, codeChallenge, request.Scopes, provider.Params)
		if err != nil {
			http.Error(w, "Failed to build auth URL", http.StatusInternalServerError)
			return
		}

		response := ConsentSpec{
			AuthURL:    authURL,
			State:      signedState,
			Scopes:     request.Scopes,
			ProviderID: request.ProviderID,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	case "api_key", "basic_auth":
		// Create Connection
		connectionID := uuid.New()
		expiresAt := time.Now().Add(10 * time.Minute)
		_, err = h.db.Exec(`
			INSERT INTO connections (id, workspace_id, provider_id, scopes, return_url, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			connectionID, request.WorkspaceID, request.ProviderID, pq.Array(request.Scopes), request.ReturnURL, expiresAt)
		if err != nil {
			http.Error(w, "Failed to create connection", http.StatusInternalServerError)
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
			http.Error(w, "Failed to sign state", http.StatusInternalServerError)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	default:
		http.Error(w, "Unsupported provider auth_type", http.StatusBadRequest)
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
	redirectPath := os.Getenv("REDIRECT_PATH")
	if redirectPath == "" {
		redirectPath = "/auth/callback"
	}

	u, err := url.Parse(providerAuthURL)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", baseURL+redirectPath)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
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
