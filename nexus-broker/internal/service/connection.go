package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/discovery"
	oidcutil "github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/oidc"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
	"github.com/google/uuid"
)

type ConnectionService interface {
	CreateConsentSpec(ctx context.Context, req CreateConsentRequest) (*ConsentSpecResponse, error)
	ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, bool, error)
	GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, string, error)
	GetTokenByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (map[string]interface{}, string, error)
	GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error)
	SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error)
	Refresh(ctx context.Context, connectionID uuid.UUID) (*RefreshResponse, error)
	ListConnections(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error)
}

type connectionService struct {
	connRepo             repository.ConnectionRepository
	tokenRepo            repository.TokenRepository
	providerStore        provider.ProfileStorer
	audit                audit.Logger
	baseURL              string
	redirectPath         string
	encryptionKey        []byte
	stateKey             []byte
	httpClient           *http.Client
	enforceReturnURL     bool
	allowedReturnDomains []string
}

type txRunner interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type CreateConsentRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	ProviderID  string   `json:"provider_id"`
	Scopes      []string `json:"scopes"`
	ReturnURL   string   `json:"return_url"`
}

type ConsentSpecResponse struct {
	AuthURL    string   `json:"authUrl"`
	State      string   `json:"state"`
	Scopes     []string `json:"scopes"`
	ProviderID string   `json:"provider_id"`
}

func NewConnectionService(
	connRepo repository.ConnectionRepository,
	tokenRepo repository.TokenRepository,
	providerStore provider.ProfileStorer,
	auditService audit.Logger,
	baseURL, redirectPath string,
	encryptionKey, stateKey []byte,
	httpClient *http.Client,
	enforceReturnURL bool,
	allowedReturnDomains []string,
) ConnectionService {
	return &connectionService{
		connRepo:             connRepo,
		tokenRepo:            tokenRepo,
		providerStore:        providerStore,
		audit:                auditService,
		baseURL:              baseURL,
		redirectPath:         redirectPath,
		encryptionKey:        encryptionKey,
		stateKey:             stateKey,
		httpClient:           httpClient,
		enforceReturnURL:     enforceReturnURL,
		allowedReturnDomains: allowedReturnDomains,
	}
}

func (s *connectionService) CreateConsentSpec(ctx context.Context, req CreateConsentRequest) (*ConsentSpecResponse, error) {
	if req.WorkspaceID == "" || req.ProviderID == "" || req.ReturnURL == "" {
		return nil, ErrBadRequest("missing_fields", "Missing required fields")
	}

	if !server.IsReturnURLAllowed(req.ReturnURL, s.enforceReturnURL, s.allowedReturnDomains) {
		return nil, ErrBadRequest("return_url_not_allowed", "return_url not allowed")
	}

	providerID, err := uuid.Parse(req.ProviderID)
	if err != nil {
		return nil, ErrBadRequest("invalid_provider_id", "Invalid provider ID")
	}

	p, err := s.providerStore.GetProfile(providerID)
	if err != nil {
		return nil, ErrNotFoundWithErr(err, "provider_not_found", "Provider not found")
	}

	switch p.AuthType {
	case "oauth2", "":
		codeVerifier, codeChallenge, err := auth.GeneratePKCE()
		if err != nil {
			return nil, ErrInternalWithErr(err, "pkce_failed", "Failed to generate PKCE")
		}

		connID := uuid.New()
		conn := &domain.Connection{
			ID:           connID,
			WorkspaceID:  req.WorkspaceID,
			ProviderID:   providerID,
			CodeVerifier: sql.NullString{String: codeVerifier, Valid: codeVerifier != ""},
			Scopes:       req.Scopes,
			ReturnURL:    req.ReturnURL,
			ExpiresAt:    time.Now().Add(10 * time.Minute),
		}

		if err := s.connRepo.Create(ctx, conn); err != nil {
			return nil, ErrInternalWithErr(err, "connection_create_failed", "Failed to create connection")
		}

		stateData := auth.StateData{
			WorkspaceID: req.WorkspaceID,
			ProviderID:  req.ProviderID,
			Nonce:       connID.String(),
			IAT:         time.Now(),
		}
		signedState, err := auth.SignState(s.stateKey, stateData)
		if err != nil {
			return nil, ErrInternalWithErr(err, "state_sign_failed", "Failed to sign state")
		}

		useAuthURL := ""
		if p.AuthURL != nil {
			useAuthURL = *p.AuthURL
		}
		hasOpenID := false
		for _, scope := range req.Scopes {
			if strings.EqualFold(scope, "openid") {
				hasOpenID = true
				break
			}
		}

		if hasOpenID && useAuthURL != "" {
			if md, errD := discovery.Discover(ctx, s.httpClient, discovery.Hint{AuthURL: useAuthURL}); errD == nil && strings.TrimSpace(md.AuthorizationEndpoint) != "" {
				useAuthURL = md.AuthorizationEndpoint
			}
		}

		clientID := ""
		if p.ClientID != nil {
			clientID = *p.ClientID
		}

		authURL, err := s.buildAuthURL(useAuthURL, clientID, signedState, codeChallenge, req.Scopes, p.Params)
		if err != nil {
			return nil, ErrInternalWithErr(err, "auth_url_failed", "Failed to build auth URL")
		}

		return &ConsentSpecResponse{
			AuthURL:    authURL,
			State:      signedState,
			Scopes:     req.Scopes,
			ProviderID: req.ProviderID,
		}, nil

	case "api_key", "basic_auth":
		connID := uuid.New()
		conn := &domain.Connection{
			ID:          connID,
			WorkspaceID: req.WorkspaceID,
			ProviderID:  providerID,
			Scopes:      req.Scopes,
			ReturnURL:   req.ReturnURL,
			ExpiresAt:   time.Now().Add(10 * time.Minute),
		}

		if err := s.connRepo.Create(ctx, conn); err != nil {
			return nil, ErrInternalWithErr(err, "connection_create_failed", "Failed to create connection")
		}

		stateData := auth.StateData{
			WorkspaceID: req.WorkspaceID,
			ProviderID:  req.ProviderID,
			Nonce:       connID.String(),
			IAT:         time.Now(),
		}
		signedState, err := auth.SignState(s.stateKey, stateData)
		if err != nil {
			return nil, ErrInternalWithErr(err, "state_sign_failed", "Failed to sign state")
		}

		captureURL, _ := url.JoinPath(s.baseURL, "/auth/capture-schema")
		u, _ := url.Parse(captureURL)
		q := u.Query()
		q.Set("state", signedState)
		u.RawQuery = q.Encode()

		return &ConsentSpecResponse{
			AuthURL:    u.String(),
			State:      signedState,
			Scopes:     req.Scopes,
			ProviderID: req.ProviderID,
		}, nil

	default:
		return nil, ErrBadRequest("unsupported_auth_type", "Unsupported provider auth_type")
	}
}

func (s *connectionService) ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, bool, error) {
	if errorParam != "" {
		return "", false, fmt.Errorf("oauth error: %s - %s", errorParam, errorDesc)
	}

	stateData, err := auth.VerifyState(s.stateKey, state)
	if err != nil {
		return "", false, ErrBadRequestWithErr(err, "invalid_state", "Invalid state")
	}

	connID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		return "", false, ErrBadRequest("invalid_connection_id", "Invalid connection ID in state")
	}

	conn, err := s.connRepo.GetPending(ctx, connID)
	if err != nil {
		return "", false, ErrNotFoundWithErr(err, "connection_not_found", "Connection not found or expired")
	}

	p, err := s.providerStore.GetProfile(conn.ProviderID)
	if err != nil {
		return "", false, ErrInternalWithErr(err, "provider_not_found", "Provider not found")
	}

	redirectURI, _ := url.JoinPath(s.baseURL, s.redirectPath)

	skipScopeOnExchange := false
	if p.Params != nil {
		var paramsMap map[string]interface{}
		if err := json.Unmarshal(*p.Params, &paramsMap); err == nil {
			if skip, ok := paramsMap["skip_scope_on_exchange"].(bool); ok {
				skipScopeOnExchange = skip
			}
		}
	}

	useTokenURL := ""
	if p.TokenURL != nil {
		useTokenURL = *p.TokenURL
	}
	if md, errD := discovery.Discover(ctx, s.httpClient, discovery.Hint{AuthURL: useTokenURL}); errD == nil && strings.TrimSpace(md.TokenEndpoint) != "" {
		useTokenURL = md.TokenEndpoint
	}

	clientID := ""
	if p.ClientID != nil {
		clientID = *p.ClientID
	}
	clientSecret := ""
	if p.ClientSecret != nil {
		clientSecret = *p.ClientSecret
	}

	tokens, err := s.executeExchange(ctx, useTokenURL, clientID, clientSecret, code, conn.CodeVerifier.String, redirectURI, conn.Scopes, p.AuthHeader, skipScopeOnExchange)
	if err != nil {
		s.connRepo.UpdateStatus(ctx, connID, "failed")
		return "", false, ErrInternalWithErr(err, "token_exchange_failed", "Token exchange failed")
	}

	hasIDToken := false
	if raw, ok := tokens["id_token"].(string); ok && raw != "" {
		hasIDToken = true
		if containsScope(conn.Scopes, "openid") {
			if _, err := oidcutil.VerifyIDToken(ctx, s.httpClient, raw, clientID, state); err != nil {
				s.connRepo.UpdateStatus(ctx, connID, "failed")
				return "", false, ErrBadRequest("invalid_id_token", "Invalid id_token")
			}
		}
	}

	tokenJSON, err := json.Marshal(tokens)
	if err != nil {
		return "", false, ErrInternalWithErr(err, "token_marshal_failed", "Failed to serialize token response")
	}
	encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
	if err != nil {
		return "", false, ErrInternalWithErr(err, "encryption_failed", "Failed to encrypt tokens")
	}

	var expiresAt *time.Time
	if expiresIn, ok := tokens["expires_in"].(float64); ok {
		expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &expiry
	}

	if err := s.inTx(ctx, func(txCtx context.Context) error {
		if err := s.tokenRepo.Upsert(txCtx, &domain.Token{
			ConnectionID:  connID,
			EncryptedData: encryptedData,
			ExpiresAt:     expiresAt,
		}); err != nil {
			return ErrInternalWithErr(err, "token_store_failed", "Failed to store tokens")
		}
		if err := s.connRepo.UpdateStatus(txCtx, connID, "active"); err != nil {
			return ErrInternalWithErr(err, "status_update_failed", "Failed to update status")
		}
		if err := s.connRepo.DeactivateOtherActive(txCtx, stateData.WorkspaceID, conn.ProviderID, connID); err != nil {
			return ErrInternalWithErr(err, "deactivate_stale_failed", "Failed to deactivate stale connections")
		}
		return nil
	}); err != nil {
		return "", false, err
	}

	if !server.IsReturnURLAllowed(conn.ReturnURL, s.enforceReturnURL, s.allowedReturnDomains) {
		return "", false, ErrBadRequest("return_url_not_allowed", "return_url not allowed")
	}

	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		return "", false, fmt.Errorf("invalid return_url")
	}

	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connID.String())
	query.Set("provider", p.Name)
	returnURL.RawQuery = query.Encode()

	return returnURL.String(), hasIDToken, nil
}

func (s *connectionService) inTx(ctx context.Context, fn func(context.Context) error) error {
	if runner, ok := s.connRepo.(txRunner); ok {
		return runner.InTx(ctx, fn)
	}
	return fn(ctx)
}

func (s *connectionService) GetTokenByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (map[string]interface{}, string, error) {
	conn, err := s.connRepo.GetActiveByWorkspaceAndProvider(ctx, workspaceID, providerName)
	if err != nil {
		return nil, "", ErrNotFoundWithErr(err, "connection_not_found", "Active connection not found for workspace and provider")
	}

	return s.GetToken(ctx, conn.ID)
}

func (s *connectionService) GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, string, error) {
	conn, err := s.connRepo.GetWithProvider(ctx, connectionID)
	if err != nil {
		return nil, "", ErrNotFoundWithErr(err, "connection_not_found", "Connection not found")
	}

	if conn.Status != "active" {
		if conn.Status == "attention" {
			return nil, "", ErrConflict("attention_required", "Connection requires attention. The user must re-authenticate.")
		}
		return nil, "", ErrBadRequest("connection_not_active", "Connection not active")
	}

	token, err := s.tokenRepo.Get(ctx, connectionID)
	if err != nil {
		return nil, "", ErrNotFoundWithErr(err, "token_not_found", "Token not found")
	}

	decryptedData, err := vault.Decrypt(s.encryptionKey, token.EncryptedData)
	if err != nil {
		return nil, "", ErrInternalWithErr(err, "decrypt_failed", "Failed to decrypt token")
	}

	var credentials map[string]interface{}
	if err := json.Unmarshal(decryptedData, &credentials); err != nil {
		return nil, "", ErrInternalWithErr(err, "invalid_token_format", "Invalid token format")
	}

	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		if conn.AuthType == "oauth2" || conn.AuthType == "" {
			if _, refreshErr := s.Refresh(ctx, connectionID); refreshErr != nil {
				return nil, "", ErrConflict("token_expired", "Token has expired and could not be refreshed. Re-authentication is required.")
			}
			token, err = s.tokenRepo.Get(ctx, connectionID)
			if err != nil {
				return nil, "", ErrInternalWithErr(err, "token_not_found", "Failed to fetch refreshed token")
			}
			decryptedData, err = vault.Decrypt(s.encryptionKey, token.EncryptedData)
			if err != nil {
				return nil, "", ErrInternalWithErr(err, "decrypt_failed", "Failed to decrypt refreshed token")
			}
			if err = json.Unmarshal(decryptedData, &credentials); err != nil {
				return nil, "", ErrInternalWithErr(err, "invalid_token_format", "Invalid refreshed token format")
			}
		}
	}

	if token.ExpiresAt != nil {
		credentials["expires_at"] = token.ExpiresAt.Format(time.RFC3339)
		credentials["expired"] = token.ExpiresAt.Before(time.Now())
	}

	response := make(map[string]interface{})
	var strategy map[string]interface{}

	if conn.AuthType == "oauth2" || conn.AuthType == "" {
		strategy = map[string]interface{}{
			"type": "oauth2",
		}
		for k, v := range credentials {
			response[k] = v
		}
	} else {
		foundStrategy := false
		if conn.ProviderParams != nil {
			var paramsMap map[string]interface{}
			if err := json.Unmarshal(*conn.ProviderParams, &paramsMap); err == nil {
				if st, ok := paramsMap["auth_strategy"].(map[string]interface{}); ok {
					strategy = st
					foundStrategy = true
				}
			}
		}
		if !foundStrategy {
			switch conn.AuthType {
			case "api_key":
				strategy = map[string]interface{}{"type": "header", "config": map[string]string{"header_name": "X-API-Key", "credential_field": "api_key"}}
			case "basic_auth":
				strategy = map[string]interface{}{"type": "basic_auth"}
			default:
				strategy = map[string]interface{}{"type": conn.AuthType}
			}
		}
	}

	response["strategy"] = strategy
	response["credentials"] = credentials
	response["health_status"] = conn.HealthStatus
	// Resolve the base URL the caller should target: the provider's configured
	// api_base_url, or the user-supplied instance URL for self-hosted providers.
	if base := effectiveBaseURL(conn.APIBaseURL, credentials); base != "" {
		response["api_base_url"] = base
	}

	return response, conn.ProviderName, nil
}

func (s *connectionService) ListConnections(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error) {
	if workspaceID == "" {
		return nil, ErrBadRequest("missing_workspace_id", "workspace_id is required")
	}
	return s.connRepo.ListByWorkspace(ctx, workspaceID)
}

// Helpers

func (s *connectionService) buildAuthURL(providerAuthURL, clientID, state, codeChallenge string, scopes []string, providerParams *json.RawMessage) (string, error) {
	redirectURI, _ := url.JoinPath(s.baseURL, s.redirectPath)

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
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")

	if !skipScopeOnAuth {
		if len(scopes) > 0 {
			q.Set("scope", strings.Join(scopes, " "))
		} else {
			q.Set("scope", "")
		}
	}

	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")

	for _, scope := range scopes {
		if strings.EqualFold(scope, "openid") {
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

func (s *connectionService) executeExchange(ctx context.Context, tokenURL, clientID, clientSecret, code, codeVerifier, redirectURI string, scopes []string, authHeader string, skipScopeOnExchange bool) (map[string]interface{}, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}
	data.Set("redirect_uri", redirectURI)

	useBasicAuth := false
	if strings.EqualFold(authHeader, "client_secret_basic") || strings.EqualFold(authHeader, "Basic") {
		useBasicAuth = true
	} else {
		if clientID != "" {
			data.Set("client_id", clientID)
		}
		if clientSecret != "" {
			data.Set("client_secret", clientSecret)
		}
	}

	if len(scopes) > 0 && !skipScopeOnExchange {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := s.httpClient.Do(req)
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

func containsScope(scopes []string, target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	for _, s := range scopes {
		if strings.ToLower(strings.TrimSpace(s)) == t {
			return true
		}
	}
	return false
}
