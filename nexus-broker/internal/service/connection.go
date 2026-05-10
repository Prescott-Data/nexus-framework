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

	"github.com/google/uuid"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/discovery"
	oidcutil "github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/oidc"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

type ConnectionService interface {
	CreateConsentSpec(ctx context.Context, req CreateConsentRequest) (*ConsentSpecResponse, error)
	ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, error)
	GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, error)
	GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error)
	SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error)
	Refresh(ctx context.Context, connectionID uuid.UUID) (*RefreshResponse, error)
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
		return nil, fmt.Errorf("missing required fields")
	}

	if !server.IsReturnURLAllowed(req.ReturnURL, s.enforceReturnURL, s.allowedReturnDomains) {
		return nil, fmt.Errorf("return_url not allowed")
	}

	providerID, err := uuid.Parse(req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("invalid provider ID")
	}

	p, err := s.providerStore.GetProfile(providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	switch p.AuthType {
	case "oauth2", "":
		codeVerifier, codeChallenge, err := auth.GeneratePKCE()
		if err != nil {
			return nil, fmt.Errorf("failed to generate PKCE: %w", err)
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
			return nil, fmt.Errorf("failed to create connection: %w", err)
		}

		stateData := auth.StateData{
			WorkspaceID: req.WorkspaceID,
			ProviderID:  req.ProviderID,
			Nonce:       connID.String(),
			IAT:         time.Now(),
		}
		signedState, err := auth.SignState(s.stateKey, stateData)
		if err != nil {
			return nil, fmt.Errorf("failed to sign state: %w", err)
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
			return nil, fmt.Errorf("failed to build auth URL: %w", err)
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
			return nil, fmt.Errorf("failed to create connection: %w", err)
		}

		stateData := auth.StateData{
			WorkspaceID: req.WorkspaceID,
			ProviderID:  req.ProviderID,
			Nonce:       connID.String(),
			IAT:         time.Now(),
		}
		signedState, err := auth.SignState(s.stateKey, stateData)
		if err != nil {
			return nil, fmt.Errorf("failed to sign state: %w", err)
		}

		brokerBaseURL := strings.TrimSuffix(s.baseURL, "")
		capturePath := "/auth/capture-schema"
		u, _ := url.Parse(brokerBaseURL + capturePath)
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
		return nil, fmt.Errorf("unsupported provider auth_type")
	}
}

func (s *connectionService) ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, error) {
	if errorParam != "" {
		return "", fmt.Errorf("oauth error: %s - %s", errorParam, errorDesc)
	}

	stateData, err := auth.VerifyState(s.stateKey, state)
	if err != nil {
		return "", fmt.Errorf("invalid state: %w", err)
	}

	connID, err := uuid.Parse(stateData.Nonce)
	if err != nil {
		return "", fmt.Errorf("invalid connection ID in state")
	}

	conn, err := s.connRepo.GetPending(ctx, connID)
	if err != nil {
		return "", fmt.Errorf("connection not found or expired")
	}

	p, err := s.providerStore.GetProfile(conn.ProviderID)
	if err != nil {
		return "", fmt.Errorf("provider not found")
	}

	base := strings.TrimSuffix(s.baseURL, "/")
	redirectURI := base + s.redirectPath

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
	if md, errD := discovery.Discover(ctx, s.httpClient, discovery.Hint{AuthURL: useTokenURL}); errD == nil && strings.TrimSpace(md.AuthorizationEndpoint) != "" {
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

	tokens, err := s.executeExchange(useTokenURL, clientID, clientSecret, code, conn.CodeVerifier.String, redirectURI, conn.Scopes, p.AuthHeader, skipScopeOnExchange)
	if err != nil {
		s.connRepo.UpdateStatus(ctx, connID, "failed")
		return "", fmt.Errorf("token exchange failed: %w", err)
	}

	if raw, ok := tokens["id_token"].(string); ok && raw != "" {
		if containsScope(conn.Scopes, "openid") {
			if _, err := oidcutil.VerifyIDToken(ctx, s.httpClient, raw, clientID, state); err != nil {
				s.connRepo.UpdateStatus(ctx, connID, "failed")
				return "", fmt.Errorf("invalid id_token")
			}
		}
	}

	tokenJSON, _ := json.Marshal(tokens)
	encryptedData, err := vault.Encrypt(s.encryptionKey, tokenJSON)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt tokens: %w", err)
	}

	var expiresAt *time.Time
	if expiresIn, ok := tokens["expires_in"].(float64); ok {
		expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &expiry
	}

	err = s.tokenRepo.Upsert(ctx, &domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptedData,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to store tokens: %w", err)
	}

	s.connRepo.UpdateStatus(ctx, connID, "active")

	if !server.IsReturnURLAllowed(conn.ReturnURL, s.enforceReturnURL, s.allowedReturnDomains) {
		return "", fmt.Errorf("return_url not allowed")
	}

	returnURL, err := url.Parse(conn.ReturnURL)
	if err != nil {
		return "", fmt.Errorf("invalid return_url")
	}

	query := returnURL.Query()
	query.Set("status", "success")
	query.Set("connection_id", connID.String())
	query.Set("provider", p.Name)
	returnURL.RawQuery = query.Encode()

	return returnURL.String(), nil
}

func (s *connectionService) GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, error) {
	conn, err := s.connRepo.GetWithProvider(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("connection not found")
	}

	if conn.Status != "active" {
		if conn.Status == "attention" {
			return nil, fmt.Errorf("attention_required")
		}
		return nil, fmt.Errorf("connection not active")
	}

	token, err := s.tokenRepo.Get(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("token not found")
	}

	decryptedData, err := vault.Decrypt(s.encryptionKey, token.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token")
	}

	var credentials map[string]interface{}
	if err := json.Unmarshal(decryptedData, &credentials); err != nil {
		return nil, fmt.Errorf("invalid token format")
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

	return response, nil
}

// Helpers

func (s *connectionService) buildAuthURL(providerAuthURL, clientID, state, codeChallenge string, scopes []string, providerParams *json.RawMessage) (string, error) {
	baseURL := strings.TrimSuffix(s.baseURL, "/")
	redirectPath := s.redirectPath

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

func (s *connectionService) executeExchange(tokenURL, clientID, clientSecret, code, codeVerifier, redirectURI string, scopes []string, authHeader string, skipScopeOnExchange bool) (map[string]interface{}, error) {
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

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
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
