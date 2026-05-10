package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/auth"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

// MockConnectionRepository is a mock of repository.ConnectionRepository
type MockConnectionRepository struct {
	mock.Mock
}

func (m *MockConnectionRepository) Create(ctx context.Context, conn *domain.Connection) error {
	args := m.Called(ctx, conn)
	return args.Error(0)
}

func (m *MockConnectionRepository) GetPending(ctx context.Context, id uuid.UUID) (*domain.Connection, error) {
	args := m.Called(ctx, id)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.Connection), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionRepository) GetWithProvider(ctx context.Context, id uuid.UUID) (*domain.ConnectionWithProvider, error) {
	args := m.Called(ctx, id)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.ConnectionWithProvider), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionRepository) GetReturnURL(ctx context.Context, id uuid.UUID) (string, error) {
	args := m.Called(ctx, id)
	return args.String(0), args.Error(1)
}

func (m *MockConnectionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

// MockTokenRepository is a mock of repository.TokenRepository
type MockTokenRepository struct {
	mock.Mock
}

func (m *MockTokenRepository) Upsert(ctx context.Context, token *domain.Token) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockTokenRepository) Get(ctx context.Context, connectionID uuid.UUID) (*domain.Token, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.Token), args.Error(1)
	}
	return nil, args.Error(1)
}

// MockProfileStorer is a mock of provider.ProfileStorer
type MockProfileStorer struct {
	mock.Mock
}

func (m *MockProfileStorer) RegisterProfile(jsonStr string) (*provider.Profile, error) {
	args := m.Called(jsonStr)
	if args.Get(0) != nil {
		return args.Get(0).(*provider.Profile), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockProfileStorer) GetProfile(id uuid.UUID) (*provider.Profile, error) {
	args := m.Called(id)
	if args.Get(0) != nil {
		return args.Get(0).(*provider.Profile), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockProfileStorer) UpdateProfile(p *provider.Profile) error {
	args := m.Called(p)
	return args.Error(0)
}

func (m *MockProfileStorer) PatchProfile(id uuid.UUID, updates map[string]interface{}) error {
	args := m.Called(id, updates)
	return args.Error(0)
}

func (m *MockProfileStorer) DeleteProfile(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockProfileStorer) GetProfileByName(name string) (*provider.Profile, error) {
	args := m.Called(name)
	if args.Get(0) != nil {
		return args.Get(0).(*provider.Profile), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockProfileStorer) DeleteProfileByName(name string) (int64, error) {
	args := m.Called(name)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockProfileStorer) ListProfiles() ([]provider.ProfileList, error) {
	args := m.Called()
	if args.Get(0) != nil {
		return args.Get(0).([]provider.ProfileList), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockProfileStorer) GetMetadata() (map[string]map[string]interface{}, error) {
	args := m.Called()
	if args.Get(0) != nil {
		return args.Get(0).(map[string]map[string]interface{}), args.Error(1)
	}
	return nil, args.Error(1)
}

func ptr(s string) *string {
	return &s
}

func setupTestService(t *testing.T) (*MockConnectionRepository, *MockTokenRepository, *MockProfileStorer, service.ConnectionService) {
	connRepo := new(MockConnectionRepository)
	tokenRepo := new(MockTokenRepository)
	providerStore := new(MockProfileStorer)

	encryptionKey := []byte("12345678901234567890123456789012") // 32 bytes
	stateKey := []byte("12345678901234567890123456789012")      // 32 bytes

	svc := service.NewConnectionService(
		connRepo,
		tokenRepo,
		providerStore,
		nil, // Skip audit service for pure unit tests
		"http://localhost:8080",
		"/auth/callback",
		encryptionKey,
		stateKey,
		http.DefaultClient,
		false, // enforceReturnURL
		[]string{},
	)

	return connRepo, tokenRepo, providerStore, svc
}

func TestConnectionService_CreateConsentSpec_OAuth2(t *testing.T) {
	connRepo, _, providerStore, svc := setupTestService(t)

	req := service.CreateConsentRequest{
		WorkspaceID: "ws-123",
		ProviderID:  uuid.New().String(),
		Scopes:      []string{"read", "write"},
		ReturnURL:   "http://app.example.com/callback",
	}

	providerID := uuid.MustParse(req.ProviderID)
	prof := &provider.Profile{
		ID:       providerID,
		Name:     "TestOAuth",
		AuthType: "oauth2",
		AuthURL:  ptr("https://provider.com/auth"),
		ClientID: ptr("client-123"),
	}

	providerStore.On("GetProfile", providerID).Return(prof, nil)
	connRepo.On("Create", mock.Anything, mock.MatchedBy(func(c *domain.Connection) bool {
		return c.WorkspaceID == req.WorkspaceID && c.ProviderID == providerID && c.ReturnURL == req.ReturnURL
	})).Return(nil)

	resp, err := svc.CreateConsentSpec(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, req.ProviderID, resp.ProviderID)
	assert.Contains(t, resp.AuthURL, "https://provider.com/auth")
	assert.Contains(t, resp.AuthURL, "client_id=client-123")
	assert.Contains(t, resp.AuthURL, "redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fauth%2Fcallback")
	assert.Contains(t, resp.AuthURL, "response_type=code")
	assert.Contains(t, resp.AuthURL, "scope=read+write")
	assert.NotEmpty(t, resp.State)

	providerStore.AssertExpectations(t)
	connRepo.AssertExpectations(t)
}

func TestConnectionService_CreateConsentSpec_ApiKey(t *testing.T) {
	connRepo, _, providerStore, svc := setupTestService(t)

	req := service.CreateConsentRequest{
		WorkspaceID: "ws-123",
		ProviderID:  uuid.New().String(),
		ReturnURL:   "http://app.example.com/callback",
	}

	providerID := uuid.MustParse(req.ProviderID)
	prof := &provider.Profile{
		ID:       providerID,
		Name:     "TestAPIKey",
		AuthType: "api_key",
	}

	providerStore.On("GetProfile", providerID).Return(prof, nil)
	connRepo.On("Create", mock.Anything, mock.MatchedBy(func(c *domain.Connection) bool {
		return c.WorkspaceID == req.WorkspaceID && c.ProviderID == providerID
	})).Return(nil)

	resp, err := svc.CreateConsentSpec(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, req.ProviderID, resp.ProviderID)
	assert.Contains(t, resp.AuthURL, "http://localhost:8080/auth/capture-schema")
	assert.Contains(t, resp.AuthURL, "state=")
	assert.NotEmpty(t, resp.State)

	providerStore.AssertExpectations(t)
	connRepo.AssertExpectations(t)
}

func TestConnectionService_GetToken_Active(t *testing.T) {
	connRepo, tokenRepo, _, svc := setupTestService(t)

	connID := uuid.New()
	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "oauth2",
	}

	expiresAt := time.Now().Add(1 * time.Hour)
	tokenData := map[string]interface{}{"access_token": "super-secret-token"}
	tokenBytes, _ := json.Marshal(tokenData)
	
	encryptionKey := []byte("12345678901234567890123456789012") 
	encryptedData, _ := vault.Encrypt(encryptionKey, tokenBytes)

	token := &domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptedData,
		ExpiresAt:     &expiresAt,
	}

	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)
	tokenRepo.On("Get", mock.Anything, connID).Return(token, nil)

	resp, err := svc.GetToken(context.Background(), connID)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	
	strategy := resp["strategy"].(map[string]interface{})
	assert.Equal(t, "oauth2", strategy["type"])

	creds := resp["credentials"].(map[string]interface{})
	assert.Equal(t, "super-secret-token", creds["access_token"])
	assert.False(t, creds["expired"].(bool))

	connRepo.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
}

func TestConnectionService_GetToken_NotActive(t *testing.T) {
	connRepo, _, _, svc := setupTestService(t)

	connID := uuid.New()
	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "pending",
		},
	}

	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)

	resp, err := svc.GetToken(context.Background(), connID)

	assert.Error(t, err)
	assert.Nil(t, resp)

	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "connection_not_active", svcErr.Code)

	connRepo.AssertExpectations(t)
}

// --- setupTestServiceWithHTTPClient creates a service with a custom HTTP client (for mock servers) ---

func setupTestServiceWithHTTPClient(t *testing.T, httpClient *http.Client) (*MockConnectionRepository, *MockTokenRepository, *MockProfileStorer, service.ConnectionService, []byte) {
	connRepo := new(MockConnectionRepository)
	tokenRepo := new(MockTokenRepository)
	providerStore := new(MockProfileStorer)

	encryptionKey := []byte("12345678901234567890123456789012")
	stateKey := []byte("12345678901234567890123456789012")

	svc := service.NewConnectionService(
		connRepo,
		tokenRepo,
		providerStore,
		nil,
		"http://localhost:8080",
		"/auth/callback",
		encryptionKey,
		stateKey,
		httpClient,
		false,
		[]string{},
	)

	return connRepo, tokenRepo, providerStore, svc, stateKey
}

// =============================================================================
// SaveCredential Tests
// =============================================================================

func TestConnectionService_SaveCredential(t *testing.T) {
	connRepo, tokenRepo, _, svc, stateKey := setupTestServiceWithHTTPClient(t, http.DefaultClient)

	connID := uuid.New()
	providerID := uuid.New()

	// Sign a valid state
	stateData := auth.StateData{
		WorkspaceID: "ws-test",
		ProviderID:  providerID.String(),
		Nonce:       connID.String(),
		IAT:         time.Now(),
	}
	signedState, err := auth.SignState(stateKey, stateData)
	assert.NoError(t, err)

	// Mock: GetWithProvider returns a connection with NO userInfoEndpoint (skip validation)
	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "pending",
			ReturnURL:  "http://app.example.com/callback",
		},
		AuthType:         "api_key",
		APIBaseURL:       "",
		UserInfoEndpoint: "",
	}
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)
	tokenRepo.On("Upsert", mock.Anything, mock.MatchedBy(func(t *domain.Token) bool {
		return t.ConnectionID == connID && t.EncryptedData != ""
	})).Return(nil)
	connRepo.On("UpdateStatus", mock.Anything, connID, "active").Return(nil)

	credentials := map[string]interface{}{"api_key": "my-secret-key"}
	returnURL, err := svc.SaveCredential(context.Background(), signedState, credentials)

	assert.NoError(t, err)
	assert.Contains(t, returnURL, "http://app.example.com/callback")
	assert.Contains(t, returnURL, "status=success")
	assert.Contains(t, returnURL, "connection_id="+connID.String())

	connRepo.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
}

func TestConnectionService_SaveCredential_InvalidState(t *testing.T) {
	_, _, _, svc := setupTestService(t)

	_, err := svc.SaveCredential(context.Background(), "garbage-state", map[string]interface{}{"api_key": "test"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state")
}

// =============================================================================
// Refresh Tests
// =============================================================================

func TestConnectionService_Refresh_StaticToken(t *testing.T) {
	connRepo, _, _, svc := setupTestService(t)

	connID := uuid.New()
	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "api_key",
	}
	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)

	resp, err := svc.Refresh(context.Background(), connID)

	assert.Error(t, err)
	assert.Nil(t, resp)

	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "static_token", svcErr.Code)

	connRepo.AssertExpectations(t)
}

func TestConnectionService_Refresh_NoRefreshToken(t *testing.T) {
	connRepo, tokenRepo, providerStore, svc := setupTestService(t)

	connID := uuid.New()
	providerID := uuid.New()
	encryptionKey := []byte("12345678901234567890123456789012")

	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "active",
		},
		AuthType: "oauth2",
	}

	prof := &provider.Profile{
		ID:           providerID,
		TokenURL:     ptr("https://provider.com/token"),
		ClientID:     ptr("client-id"),
		ClientSecret: ptr("client-secret"),
	}

	// Stored token has no refresh_token
	tokenData := map[string]interface{}{"access_token": "at-12345"}
	tokenBytes, _ := json.Marshal(tokenData)
	encryptedData, _ := vault.Encrypt(encryptionKey, tokenBytes)

	token := &domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptedData,
	}

	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)
	providerStore.On("GetProfile", providerID).Return(prof, nil)
	tokenRepo.On("Get", mock.Anything, connID).Return(token, nil)

	resp, err := svc.Refresh(context.Background(), connID)

	assert.Error(t, err)
	assert.Nil(t, resp)

	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "no_refresh_token", svcErr.Code)

	connRepo.AssertExpectations(t)
	providerStore.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
}

func TestConnectionService_Refresh_OAuth2_Success(t *testing.T) {
	// Mock token endpoint that returns fresh tokens
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-at-99999",
			"refresh_token": "new-rt-88888",
			"expires_in":    3600,
		})
	}))
	defer mockServer.Close()

	connRepo, tokenRepo, providerStore, svc, _ := setupTestServiceWithHTTPClient(t, mockServer.Client())

	connID := uuid.New()
	providerID := uuid.New()
	encryptionKey := []byte("12345678901234567890123456789012")

	connWithProv := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "active",
		},
		AuthType: "oauth2",
	}

	prof := &provider.Profile{
		ID:           providerID,
		TokenURL:     ptr(mockServer.URL),
		ClientID:     ptr("client-id"),
		ClientSecret: ptr("client-secret"),
	}

	// Stored token has a refresh_token
	tokenData := map[string]interface{}{
		"access_token":  "old-at-11111",
		"refresh_token": "old-rt-22222",
	}
	tokenBytes, _ := json.Marshal(tokenData)
	encryptedData, _ := vault.Encrypt(encryptionKey, tokenBytes)
	token := &domain.Token{
		ConnectionID:  connID,
		EncryptedData: encryptedData,
	}

	connRepo.On("GetWithProvider", mock.Anything, connID).Return(connWithProv, nil)
	providerStore.On("GetProfile", providerID).Return(prof, nil)
	tokenRepo.On("Get", mock.Anything, connID).Return(token, nil)
	tokenRepo.On("Upsert", mock.Anything, mock.MatchedBy(func(t *domain.Token) bool {
		return t.ConnectionID == connID && t.EncryptedData != ""
	})).Return(nil)

	resp, err := svc.Refresh(context.Background(), connID)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "new-at-99999", resp.Tokens["access_token"])
	assert.Equal(t, "new-rt-88888", resp.Tokens["refresh_token"])

	connRepo.AssertExpectations(t)
	providerStore.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
}

// =============================================================================
// ExchangeCodeForTokens Tests
// =============================================================================

func TestConnectionService_ExchangeCodeForTokens_InvalidState(t *testing.T) {
	_, _, _, svc := setupTestService(t)

	_, _, err := svc.ExchangeCodeForTokens(context.Background(), "bad-state", "some-code", "", "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state")
}

func TestConnectionService_ExchangeCodeForTokens_Success(t *testing.T) {
	// Mock token endpoint
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "at-exchanged",
			"refresh_token": "rt-exchanged",
			"expires_in":    7200,
		})
	}))
	defer mockServer.Close()

	connRepo, tokenRepo, providerStore, svc, stateKey := setupTestServiceWithHTTPClient(t, mockServer.Client())

	connID := uuid.New()
	providerID := uuid.New()

	// Sign a valid state
	stateData := auth.StateData{
		WorkspaceID: "ws-exchange",
		ProviderID:  providerID.String(),
		Nonce:       connID.String(),
		IAT:         time.Now(),
	}
	signedState, err := auth.SignState(stateKey, stateData)
	assert.NoError(t, err)

	// Mock GetPending
	conn := &domain.Connection{
		ID:           connID,
		ProviderID:   providerID,
		CodeVerifier: sql.NullString{String: "test-verifier", Valid: true},
		Scopes:       []string{"read"},
		ReturnURL:    "http://app.example.com/done",
	}
	connRepo.On("GetPending", mock.Anything, connID).Return(conn, nil)

	// Mock GetProfile
	prof := &provider.Profile{
		ID:           providerID,
		Name:         "test-provider",
		AuthType:     "oauth2",
		TokenURL:     ptr(mockServer.URL),
		ClientID:     ptr("client-123"),
		ClientSecret: ptr("secret-456"),
	}
	providerStore.On("GetProfile", providerID).Return(prof, nil)

	// Mock token storage
	tokenRepo.On("Upsert", mock.Anything, mock.MatchedBy(func(t *domain.Token) bool {
		return t.ConnectionID == connID && t.EncryptedData != ""
	})).Return(nil)

	// Mock status update
	connRepo.On("UpdateStatus", mock.Anything, connID, "active").Return(nil)

	returnURL, _, err := svc.ExchangeCodeForTokens(context.Background(), signedState, "auth-code-xyz", "", "")

	assert.NoError(t, err)
	assert.Contains(t, returnURL, "http://app.example.com/done")
	assert.Contains(t, returnURL, "status=success")
	assert.Contains(t, returnURL, fmt.Sprintf("connection_id=%s", connID))
	assert.Contains(t, returnURL, "provider=test-provider")

	connRepo.AssertExpectations(t)
	providerStore.AssertExpectations(t)
	tokenRepo.AssertExpectations(t)
}
