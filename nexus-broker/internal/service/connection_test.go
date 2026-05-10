package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
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
	assert.Equal(t, "connection not active", err.Error())

	connRepo.AssertExpectations(t)
}
