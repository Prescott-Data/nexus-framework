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

	"dromos-oauth-broker/internal/auth"
	"dromos-oauth-broker/internal/discovery"
	oidcutil "dromos-oauth-broker/internal/oidc"
	"dromos-oauth-broker/internal/server"
	"dromos-oauth-broker/internal/vault"
)

// CallbackHandler handles OAuth callback and token exchange
type CallbackHandler struct {
	db                    *sqlx.DB
	encryptionKey         []byte
	stateKey              []byte
	metricExchangeSuccess prometheus.Counter
	metricExchangeError   prometheus.Counter
	histogramExchangeDur  prometheus.Histogram
	metricIDTokens        prometheus.Counter
	metricTokenGet        *prometheus.CounterVec
}

// NewCallbackHandler creates a new callback handler
func NewCallbackHandler(db *sqlx.DB, encryptionKey, stateKey []byte) *CallbackHandler {
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

	prometheus.MustRegister(success, failure, hist, idTokens, tokenGet)
	return &CallbackHandler{
		db:                    db,
		encryptionKey:         encryptionKey,
		stateKey:              stateKey,
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
	}

	err = h.db.QueryRow(`
		SELECT token_url, client_id, client_secret
		FROM provider_profiles WHERE id = $1`,
		connection.ProviderID).Scan(&provider.TokenURL, &provider.ClientID, &provider.ClientSecret)

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
	if md, errD := discovery.Discover(r.Context(), discovery.Hint{AuthURL: provider.TokenURL}); errD == nil && strings.TrimSpace(md.TokenEndpoint) != "" {
		useTokenURL = md.TokenEndpoint
	}
	tokens, err := h.exchangeCodeForTokens(useTokenURL, provider.ClientID, provider.ClientSecret, code, connection.CodeVerifier, redirectURI, connection.Scopes)
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
			if _, err := oidcutil.VerifyIDToken(r.Context(), raw, provider.ClientID, state); err != nil {
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
	http.Redirect(w, r, connection.ReturnURL+"?status=success&connection_id="+connectionID.String(), http.StatusFound)
}

// CaptureCredentialForm serves the HTML form for capturing static credentials.
func (h *CallbackHandler) CaptureCredentialForm(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	providerName := r.URL.Query().Get("provider_name")

	// Verify state
	_, err := auth.VerifyState(h.stateKey, state)
	if err != nil {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
		<!DOCTYPE html>
		<html>
		<head>
			<title>Enter Credentials for %s</title>
		</head>
		<body>
			<h2>Enter API Key for %s</h2>
			<form action="/auth/capture-credential" method="post">
				<input type="hidden" name="state" value="%s">
				<p><input type="text" name="credential" size="50"></p>
				<p><button type="submit">Submit</button></p>
			</form>
		</body>
		</html>
	`, providerName, providerName, state)
}

// SaveCredential handles the submission of the credential capture form.
func (h *CallbackHandler) SaveCredential(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	state := r.FormValue("state")
	credential := r.FormValue("credential")

	// Verify state
	stateData, err := auth.VerifyState(h.stateKey, state)
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

	tokens := map[string]interface{}{"access_token": credential}

	if err := h.storeTokens(connectionID, tokens); err != nil {
		http.Error(w, "Failed to store token", http.StatusInternalServerError)
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

	// Check if connection exists and is active
	var connection struct {
		Status     string `db:"status"`
		ProviderID string `db:"provider_id"`
	}

	err = h.db.QueryRow("SELECT status, provider_id FROM connections WHERE id = $1", connectionID).Scan(&connection.Status, &connection.ProviderID)
	if err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "connection not found", "id": connectionID.String()}, r)
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	if connection.Status != "active" {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "connection not active", "status": connection.Status}, r)
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

	// Parse the JSON token data
	var tokenData map[string]interface{}
	if err := json.Unmarshal(decryptedData, &tokenData); err != nil {
		h.logAuditEvent(&connectionID, "token_retrieval_failed", map[string]string{"error": "invalid token format"}, r)
		http.Error(w, "Invalid token format", http.StatusInternalServerError)
		return
	}

	// Add expiration info if available
	if token.ExpiresAt != nil {
		tokenData["expires_at"] = token.ExpiresAt.Format(time.RFC3339)
		tokenData["expired"] = token.ExpiresAt.Before(time.Now())
	}

	// Log successful retrieval
	h.logAuditEvent(&connectionID, "token_retrieved", map[string]string{}, r)

	// Emit metric for token retrieval
	hasID := "false"
	if _, ok := tokenData["id_token"]; ok {
		hasID = "true"
	}
	h.metricTokenGet.WithLabelValues(connection.ProviderID, hasID).Inc()

	// Return the decrypted token
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenData)
}

// exchangeCodeForTokens exchanges authorization code for access tokens
func (h *CallbackHandler) exchangeCodeForTokens(tokenURL, clientID, clientSecret, code, codeVerifier, redirectURI string, scopes []string) (map[string]interface{}, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("code_verifier", codeVerifier)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("redirect_uri", redirectURI)
	// Some providers (e.g., Microsoft identity platform v2) require scope on token exchange
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
func (h *CallbackHandler) refreshTokens(tokenURL, clientID, clientSecret, refreshToken string) (map[string]interface{}, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed: %s", string(body))
	}
	var tokens map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return tokens, nil
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
		newTokens, err := h.refreshTokens(provider.TokenURL, provider.ClientID, provider.ClientSecret, refreshToken)
		if err != nil {
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
