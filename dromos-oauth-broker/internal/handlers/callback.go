package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"

	"dromos.com/oauth-broker/internal/auth"
	"dromos.com/oauth-broker/internal/discovery"
	oidcutil "dromos.com/oauth-broker/internal/oidc"
	"dromos.com/oauth-broker/internal/server"
	"dromos.com/oauth-broker/internal/vault"
)

// CallbackHandler handles OAuth callback and token exchange
type CallbackHandler struct {
	db                    *sqlx.DB
	encryptionKey         []byte
	stateKey              []byte
	httpClient            *http.Client
	metricExchangeSuccess prometheus.Counter
	metricExchangeError   prometheus.Counter
	histogramExchangeDur  prometheus.Histogram
	metricIDTokens        prometheus.Counter
	metricTokenGet        *prometheus.CounterVec
}

// NewCallbackHandler creates a new callback handler
func NewCallbackHandler(db *sqlx.DB, encryptionKey, stateKey []byte, httpClient *http.Client) *CallbackHandler {
	success := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_exchanges_total",
		Help:        "Total OAuth token exchanges",
		ConstLabels: prometheus.Labels{"status": "success"},
	})
	failure := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "oauth_token_exchanges_total",
		Help:        "Total OAuth token exchanges",
		ConstLabels: prometheus.Labels{"status": "error"},
	})
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "oauth_exchange_duration_seconds",
		Help:    "Duration of token exchange requests",
		Buckets: prometheus.DefBuckets,
	})
	idTokens := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "oauth_id_tokens_returned_total",
		Help: "Total number of times an id_token was returned by provider",
	})
	tokenGet := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "oauth_token_get_total",
		Help: "Token retrievals by provider and whether id_token present",
	}, []string{"provider", "has_id_token"})

	collectors := []prometheus.Collector{success, failure, hist, idTokens, tokenGet}
	for _, c := range collectors {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}

	return &CallbackHandler{
		db:                    db,
		encryptionKey:         encryptionKey,
		stateKey:              stateKey,
		httpClient:            httpClient,
		metricExchangeSuccess: success,
		metricExchangeError:   failure,
		histogramExchangeDur:  hist,
		metricIDTokens:        idTokens,
		metricTokenGet:        tokenGet,
	}
}

// Handle handles GET /auth/callback
func (h *CallbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Get parameters from query string
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		h.handleError(w, r, errorParam, r.URL.Query().Get("error_description"))
		return
	}

	if code == "" || state == "" {
		http.Error(w, "Missing code or state parameter", http.StatusBadRequest)
		return
	}

	// Verify state
	stateData, err := auth.VerifyState(h.stateKey, state)
	if err != nil {
		h.logAuditEvent(nil, "state_verification_failed", map[string]string{"error": err.Error()}, r)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Get connection
	connectionID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		http.Error(w, "Invalid connection ID", http.StatusBadRequest)
		return
	}

	var connection struct {
		ID           string   `db:"id"`
		CodeVerifier string   `db:"code_verifier"`
		ReturnURL    string   `db:"return_url"`
		ProviderID   string   `db:"provider_id"`
		Scopes       []string `db:"scopes"`
	}

	err = h.db.QueryRow(`
		SELECT id, code_verifier, return_url, provider_id, scopes
		FROM connections
		WHERE id = $1 AND status = 'pending' AND expires_at > NOW()`,
		connectionID).Scan(&connection.ID, &connection.CodeVerifier, &connection.ReturnURL, &connection.ProviderID, pq.Array(&connection.Scopes))

	if err != nil {
		h.logAuditEvent(&connectionID, "connection_not_found", map[string]string{"error": err.Error()}, r)
		http.Error(w, "Connection not found or expired", http.StatusNotFound)
		return
	}

	// Get provider details
	var provider struct {
		TokenURL     string `db:"token_url"`
		ClientID     string `db:"client_id"`
		ClientSecret string `db:"client_secret"`
		Name         string `db:"name"`
		AuthHeader   string `db:"auth_header"`
	}

	err = h.db.QueryRow(`
		SELECT token_url, client_id, client_secret, name, COALESCE(auth_header, '') as auth_header
		FROM provider_profiles WHERE id = $1`,
		connection.ProviderID).Scan(&provider.TokenURL, &provider.ClientID, &provider.ClientSecret, &provider.Name, &provider.AuthHeader)

	if err != nil {
		h.logAuditEvent(&connectionID, "provider_not_found", map[string]string{"error": err.Error()}, r)
		http.Error(w, "Provider not found", http.StatusInternalServerError)
		return
	}

	// Compute redirect_uri to match the auth request
	redirectPath := os.Getenv("REDIRECT_PATH")
	if redirectPath == "" {
		redirectPath = "/auth/callback"
	}
	base := strings.TrimSuffix(os.Getenv("BASE_URL"), "/")
	redirectURI := base + redirectPath

	// Exchange code for tokens
	start := time.Now()
	useTokenURL := provider.TokenURL
	if md, errD := discovery.Discover(r.Context(), h.httpClient, discovery.Hint{AuthURL: provider.TokenURL}); errD == nil && strings.TrimSpace(md.TokenEndpoint) != "" {
		useTokenURL = md.TokenEndpoint
	}
	tokens, err := h.exchangeCodeForTokens(useTokenURL, provider.ClientID, provider.ClientSecret, code, connection.CodeVerifier, redirectURI, connection.Scopes, provider.AuthHeader)
	h.histogramExchangeDur.Observe(time.Since(start).Seconds())
	if err != nil {
		h.logAuditEvent(&connectionID, "token_exchange_failed", map[string]string{"error": err.Error()}, r)
		h.updateConnectionStatus(connectionID, "failed")
		h.metricExchangeError.Inc()
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}
	h.metricExchangeSuccess.Inc()
	if _, ok := tokens["id_token"]; ok {
		h.metricIDTokens.Inc()
	}

	// Verify OIDC id_token if present and openid scope requested
	if raw, ok := tokens["id_token"].(string); ok && raw != "" {
		if containsScope(connection.Scopes, "openid") {
			if _, err := oidcutil.VerifyIDToken(r.Context(), h.httpClient, raw, provider.ClientID, state); err != nil {
				h.logAuditEvent(&connectionID, "id_token_verification_failed", map[string]string{"error": err.Error()}, r)
				h.updateConnectionStatus(connectionID, "failed")
				http.Error(w, "Invalid id_token", http.StatusUnauthorized)
				return
			}
		}
	}

	// Encrypt and store tokens
	err = h.storeTokens(connectionID, tokens)
	if err != nil {
		h.logAuditEvent(&connectionID, "token_storage_failed", map[string]string{"error": err.Error()}, r)
		http.Error(w, "Failed to store tokens", http.StatusInternalServerError)
		return
	}

	// Update connection status
	err = h.updateConnectionStatus(connectionID, "active")
	if err != nil {
		h.logAuditEvent(&connectionID, "status_update_failed", map[string]string{"error": err.Error()}, r)
	}

	// Log success
	h.logAuditEvent(&connectionID, "oauth_flow_completed", map[string]string{"provider_id": connection.ProviderID}, r)

	// Redirect to return URL with success
	if !server.IsReturnURLAllowed(connection.ReturnURL) {
		http.Error(w, "return_url not allowed", http.StatusBadRequest)
		return
	}

	returnURL, err := url.Parse(connection.ReturnURL)
	if err != nil {
		h.logAuditEvent(&connectionID, "invalid_return_url", map[string]string{"error": err.Error(), "return_url": connection.ReturnURL}, r)
		http.Error(w, "Invalid return_url", http.StatusInternalServerError)
		return
	}
	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connectionID.String())
	query.Set("provider", provider.Name)
	returnURL.RawQuery = query.Encode()

	http.Redirect(w, r, returnURL.String(), http.StatusFound)
}

// GetCaptureSchema serves a JSON schema for the credential capture form.
func (h *CallbackHandler) GetCaptureSchema(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	// Verify state
	stateData, err := auth.VerifyState(h.stateKey, state)
	if err != nil {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	providerID, err := uuid.Parse(stateData.ProviderID)
	if err != nil {
		http.Error(w, "Invalid provider ID in state", http.StatusBadRequest)
		return
	}

	var provider struct {
		Name   string           `db:"name"`
		Params *json.RawMessage `db:"params"`
	}

	err = h.db.QueryRow("SELECT name, params FROM provider_profiles WHERE id = $1", providerID).Scan(&provider.Name, &provider.Params)
	if err != nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	var params map[string]json.RawMessage
	if provider.Params != nil {
		if err := json.Unmarshal(*provider.Params, &params); err != nil {
			http.Error(w, "Failed to parse provider params", http.StatusInternalServerError)
			return
		}
	}

	schema, ok := params["credential_schema"]
	if !ok {
		http.Error(w, "Credential schema not found for this provider", http.StatusNotFound)
		return
	}

	type SchemaResponse struct {
		ProviderName string          `json:"provider_name"`
		Schema       json.RawMessage `json:"schema"`
	}

	response := SchemaResponse{
		ProviderName: provider.Name,
		Schema:       schema,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SaveCredential handles the submission of the credential capture form.
func (h *CallbackHandler) SaveCredential(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		State       string                 `json:"state"`
		Credentials map[string]interface{} `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Verify state
	stateData, err := auth.VerifyState(h.stateKey, reqBody.State)
	if err != nil {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	connectionID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		http.Error(w, "Invalid connection ID", http.StatusBadRequest)
		return
	}

	var returnURL string
	err = h.db.QueryRow("SELECT return_url FROM connections WHERE id = $1", connectionID).Scan(&returnURL)
	if err != nil {
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	err = h.storeTokens(connectionID, reqBody.Credentials)
	if err != nil {
		http.Error(w, "Failed to store credentials", http.StatusInternalServerError)
		return
	}

	if err := h.updateConnectionStatus(connectionID, "active"); err != nil {
		http.Error(w, "Failed to update connection status", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, returnURL+"?status=success&connection_id="+connectionID.String(), http.StatusFound)
}

// containsScope returns true if target (case-insensitive) is present in scopes
func containsScope(scopes []string, target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	for _, s := range scopes {
		if strings.ToLower(strings.TrimSpace(s)) == t {
			return true
		}
	}
	return false
}

// GetToken handles GET /connections/{connection_id}/token
func (h *CallbackHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	// Extract connection ID from URL path
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	connectionIDStr := pathParts[len(pathParts)-2] // /connections/{id}/token

	connectionID, err := uuid.Parse(connectionIDStr)
	if err != nil {
		h.logAuditEvent(nil, "token_retrieval_failed", map[string]string{"error": "invalid connection ID", "id": connectionIDStr}, r)
		http.Error(w, "Invalid connection ID", http.StatusBadRequest)
		return
	}

	// Check if connection exists and is active, and fetch provider config
	var connection struct {
		Status     string           `db:"status"`
		ProviderID string           `db:"provider_id"`
		AuthType   string           `db:"auth_type"`
		Params     *json.RawMessage `db:"params"`
	}

	err = h.db.QueryRow(`
		SELECT c.status, c.provider_id, p.auth_type, p.params
		FROM connections c
		JOIN provider_profiles p ON c.provider_id = p.id
		WHERE c.id = $1`, connectionID).Scan(&connection.Status, &connection.ProviderID, &connection.AuthType, &connection.Params)

	if err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "connection not found or db error", "id": connectionID.String()}, r)
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	if connection.Status != "active" {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "connection not active", "status": connection.Status}, r)
		
		if connection.Status == "attention" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "attention_required", 
				"detail": "Connection requires attention. The user must re-authenticate.",
			})
			return
		}

		http.Error(w, "Connection not active", http.StatusForbidden)
		return
	}

	// Get the encrypted token
	var token struct {
		EncryptedData string     `db:"encrypted_data"`
		ExpiresAt     *time.Time `db:"expires_at"`
	}

	err = h.db.QueryRow("SELECT encrypted_data, expires_at FROM tokens WHERE connection_id = $1 ORDER BY created_at DESC LIMIT 1", connectionID).Scan(&token.EncryptedData, &token.ExpiresAt)
	if err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "token not found"}, r)
		http.Error(w, "Token not found", http.StatusNotFound)
		return
	}

	// Decrypt the token
	decryptedData, err := vault.Decrypt(h.encryptionKey, token.EncryptedData)
	if err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "decryption failed"}, r)
		http.Error(w, "Failed to decrypt token", http.StatusInternalServerError)
		return
	}

	// Parse the JSON token data (the credentials)
	var credentials map[string]interface{}
	if err := json.Unmarshal(decryptedData, &credentials); err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "invalid token format"}, r)
		http.Error(w, "Invalid token format", http.StatusInternalServerError)
		return
	}

	// Add expiration info to credentials if available (for back-compat and ease of use)
	if token.ExpiresAt != nil {
		credentials["expires_at"] = token.ExpiresAt.Format(time.RFC3339)
		credentials["expired"] = token.ExpiresAt.Before(time.Now())
	}

	// Construct the final response payload
	response := make(map[string]interface{})

	// 1. Determine Strategy
	var strategy map[string]interface{}

	if connection.AuthType == "oauth2" || connection.AuthType == "" {
		strategy = map[string]interface{}{
			"type": "oauth2",
		}
		// For backward compatibility: flatten credentials into the root for OAuth2
		for k, v := range credentials {
			response[k] = v
		}
	} else {
		// Generic Provider: Look for auth_strategy in params
		foundStrategy := false
		if connection.Params != nil {
			var paramsMap map[string]interface{}
			if err := json.Unmarshal(*connection.Params, &paramsMap); err == nil {
				if s, ok := paramsMap["auth_strategy"].(map[string]interface{}); ok {
					strategy = s
					foundStrategy = true
				}
			}
		}
		// Fallback if not explicitly defined but we know the high-level type
		if !foundStrategy {
			// Map high-level broker auth_types to default bridge strategies if possible
			// This is a "best effort" mapping if the explicit config is missing
			switch connection.AuthType {
			case "api_key":
				strategy = map[string]interface{}{"type": "header", "config": map[string]string{"header_name": "X-API-Key", "credential_field": "api_key"}}
			case "basic_auth":
				strategy = map[string]interface{}{"type": "basic_auth"}
			default:
				strategy = map[string]interface{}{"type": connection.AuthType} // Hope for the best
			}
		}
	}

	response["strategy"] = strategy
	response["credentials"] = credentials

	// Log successful retrieval
	h.logAuditEvent(&connectionID, "token_retrieved", map[string]string{}, r)

	// Emit metric for token retrieval
	hasID := "false"
	if _, ok := credentials["id_token"]; ok {
		hasID = "true"
	}
	h.metricTokenGet.WithLabelValues(connection.ProviderID, hasID).Inc()

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// exchangeCodeForTokens exchanges authorization code for access tokens
func (h *CallbackHandler) exchangeCodeForTokens(tokenURL, clientID, clientSecret, code, codeVerifier, redirectURI string, scopes []string, authHeader string) (map[string]interface{}, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("code_verifier", codeVerifier)
	data.Set("redirect_uri", redirectURI)
	
	// Determine auth method based on authHeader configuration
	// Default to "client_secret_post" (sending in body) if not specified or explicitly set
	useBasicAuth := false
	if strings.EqualFold(authHeader, "client_secret_basic") || strings.EqualFold(authHeader, "Basic") {
		useBasicAuth = true
	} else {
		// Default: Send credentials in body
		data.Set("client_id", clientID)
		data.Set("client_secret", clientSecret)
	}

	// Some providers (e.g., Microsoft identity platform v2) require scope on token exchange
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Essential for providers like GitHub that return XML by default
	req.Header.Set("Accept", "application/json")

	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokens map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}

	return tokens, nil
}

// refreshTokens refreshes using a refresh_token
func (h *CallbackHandler) refreshTokens(tokenURL, clientID, clientSecret, refreshToken string) (map[string]interface{}, int, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // Ensure JSON response

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tokens map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, resp.StatusCode, err
	}
	return tokens, resp.StatusCode, nil
}

// Refresh handles POST /connections/{connection_id}/refresh
func (h *CallbackHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Extract connection ID
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	idStr := parts[len(parts)-2]
	connectionID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid connection ID", http.StatusBadRequest)
		return
	}

	var conn struct {
		ProviderID string `db:"provider_id"`
		AuthType   string `db:"auth_type"`
	}
	err = h.db.QueryRow(`
		SELECT c.provider_id, p.auth_type 
		FROM connections c
		JOIN provider_profiles p ON c.provider_id = p.id
		WHERE c.id=$1 AND c.status='active'`, connectionID).Scan(&conn.ProviderID, &conn.AuthType)

	if err != nil {
		http.Error(w, "Connection not active or not found", http.StatusNotFound)
		return
	}

	// Check the auth type right away
	switch conn.AuthType {
	case "api_key", "basic_auth":
		// Static tokens cannot be refreshed.
		http.Error(w, "This connection uses a static token and cannot be refreshed", http.StatusBadRequest)
		return // Stop execution here
	case "oauth2", "":
		// This is an OAuth2 provider, continue with the *existing* refresh logic
		var provider struct {
			TokenURL     string `db:"token_url"`
			ClientID     string `db:"client_id"`
			ClientSecret string `db:"client_secret"`
		}
		err = h.db.QueryRow("SELECT token_url, client_id, client_secret FROM provider_profiles WHERE id=$1", conn.ProviderID).Scan(&provider.TokenURL, &provider.ClientID, &provider.ClientSecret)
		if err != nil {
			http.Error(w, "Provider not found", http.StatusInternalServerError)
			return
		}
		var tokenRow struct {
			EncryptedData string `db:"encrypted_data"`
		}
		err = h.db.QueryRow("SELECT encrypted_data FROM tokens WHERE connection_id=$1 ORDER BY created_at DESC LIMIT 1", connectionID).Scan(&tokenRow.EncryptedData)
		if err != nil {
			http.Error(w, "Token not found", http.StatusNotFound)
			return
		}
		plaintext, err := vault.Decrypt(h.encryptionKey, tokenRow.EncryptedData)
		if err != nil {
			http.Error(w, "Decrypt failed", http.StatusInternalServerError)
			return
		}
		var current map[string]interface{}
		if err := json.Unmarshal(plaintext, &current); err != nil {
			http.Error(w, "Token parse failed", http.StatusInternalServerError)
			return
		}
		refreshToken, _ := current["refresh_token"].(string)
		if refreshToken == "" {
			http.Error(w, "No refresh_token available", http.StatusBadRequest)
			return
		}
		// Refresh
		newTokens, statusCode, err := h.refreshTokens(provider.TokenURL, provider.ClientID, provider.ClientSecret, refreshToken)
		if err != nil {
			// Check for unrecoverable errors (400-499 usually implies invalid_grant, revoked, or expired)
			if statusCode >= 400 && statusCode < 500 {
				h.logAuditEvent(&connectionID, "token_refresh_fatal", map[string]string{"error": err.Error(), "status_code": fmt.Sprintf("%d", statusCode)}, r)
				h.updateConnectionStatus(connectionID, "attention")
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict) // 409 Conflict is a good signal for "state issue"
				json.NewEncoder(w).Encode(map[string]string{
					"error":  "attention_required",
					"detail": "The connection credentials are invalid or expired and cannot be refreshed. User re-consent is required.",
				})
				return
			}
			
			// For 5xx or network errors, we don't change state, just fail the request (Agent will retry)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// Store new tokens
		if err := h.storeTokens(connectionID, newTokens); err != nil {
			http.Error(w, "Store refreshed token failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newTokens)
	default:
		http.Error(w, "Unsupported provider auth_type", http.StatusInternalServerError)
		return
	}
}

// storeTokens encrypts and stores tokens in database
func (h *CallbackHandler) storeTokens(connectionID uuid.UUID, tokens map[string]interface{}) error {
	tokenJSON, err := json.Marshal(tokens)
	if err != nil {
		return err
	}

	encryptedData, err := vault.Encrypt(h.encryptionKey, tokenJSON)
	if err != nil {
		return err
	}

	// Parse expires_at if present
	var expiresAt *time.Time
	if expiresIn, ok := tokens["expires_in"].(float64); ok {
		expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &expiry
	}

	_, err = h.db.Exec(`
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		VALUES ($1, $2, $3)`,
		connectionID, encryptedData, expiresAt)

	return err
}

// updateConnectionStatus updates the connection status
func (h *CallbackHandler) updateConnectionStatus(connectionID uuid.UUID, status string) error {
	_, err := h.db.Exec("UPDATE connections SET status = $1, updated_at = NOW() WHERE id = $2", status, connectionID)
	return err
}

// logAuditEvent logs an audit event
func (h *CallbackHandler) logAuditEvent(connectionID *uuid.UUID, eventType string, data map[string]string, r *http.Request) {
	eventData, _ := json.Marshal(data)
	// Sanitize and extract client IP for inet field
	var ipVal interface{}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ip := forwarded
		if comma := strings.Index(ip, ","); comma != -1 {
			ip = strings.TrimSpace(ip[:comma])
		}
		ipVal = ip
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ipVal = host
		} else {
			ipVal = nil
		}
	}

	_, _ = h.db.Exec(`
		INSERT INTO audit_events (connection_id, event_type, event_data, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)`,
		connectionID, eventType, string(eventData), ipVal, r.Header.Get("User-Agent"))
}

// handleError handles OAuth errors
func (h *CallbackHandler) handleError(w http.ResponseWriter, r *http.Request, errorType, description string) {
	// Log the error
	h.logAuditEvent(nil, "oauth_error", map[string]string{
		"error":       errorType,
		"description": description,
	}, r)

	http.Error(w, fmt.Sprintf("OAuth error: %s - %s", errorType, description), http.StatusBadRequest)
}
