package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
)

func runWorkerUntilSignal(t *testing.T, worker *service.ConnectionHealthWorker, done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})

	go func() {
		defer close(workerDone)
		worker.Start(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		select {
		case <-workerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for health worker to stop after signal timeout")
		}
		t.Fatal("timed out waiting for health worker signal")
	}

	cancel()

	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for health worker to stop")
	}
}

// Add missing mock methods to MockConnectionRepository
func (m *MockConnectionRepository) GetForHealthCheck(ctx context.Context, limit int) ([]*domain.ConnectionWithProvider, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) != nil {
		return args.Get(0).([]*domain.ConnectionWithProvider), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionRepository) UpdateHealthStatus(ctx context.Context, id uuid.UUID, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

// MockConnectionService mocks the ConnectionService
type MockConnectionService struct {
	mock.Mock
}

func (m *MockConnectionService) CreateConsentSpec(ctx context.Context, req service.CreateConsentRequest) (*service.ConsentSpecResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*service.ConsentSpecResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionService) ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, bool, error) {
	args := m.Called(ctx, state, code, errorParam, errorDesc)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *MockConnectionService) GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, string, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(map[string]interface{}), args.String(1), args.Error(2)
	}
	return nil, args.String(1), args.Error(2)
}

func (m *MockConnectionService) GetTokenByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (map[string]interface{}, string, error) {
	args := m.Called(ctx, workspaceID, providerName)
	if args.Get(0) != nil {
		return args.Get(0).(map[string]interface{}), args.String(1), args.Error(2)
	}
	return nil, args.String(1), args.Error(2)
}

func (m *MockConnectionService) GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error) {
	args := m.Called(ctx, state)
	if args.Get(1) != nil {
		return args.String(0), args.Get(1).(json.RawMessage), args.Error(2)
	}
	return args.String(0), nil, args.Error(2)
}

func (m *MockConnectionService) SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error) {
	args := m.Called(ctx, state, credentials)
	return args.String(0), args.Error(1)
}

func (m *MockConnectionService) Refresh(ctx context.Context, connectionID uuid.UUID) (*service.RefreshResponse, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(*service.RefreshResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnectionService) ListConnections(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error) {
	args := m.Called(ctx, workspaceID)
	if args.Get(0) != nil {
		return args.Get(0).([]domain.ConnectionSummary), args.Error(1)
	}
	return nil, args.Error(1)
}

// MockProviderHealthLookup mocks the ProviderHealthLookup interface
type MockProviderHealthLookup struct {
	mock.Mock
}

func (m *MockProviderHealthLookup) GetHealthStatus(id uuid.UUID) (string, error) {
	args := m.Called(id)
	return args.String(0), args.Error(1)
}

func TestConnectionHealthWorker_OAuth2_Healthy(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	// Should do nothing after the first call since we'll cancel the context
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Mock successful refresh
	mockSvc.On("Refresh", mock.Anything, connID).Return(&service.RefreshResponse{}, nil).Once()

	// Should update health to healthy
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "healthy").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestConnectionHealthWorker_OAuth2_Expired(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	providerID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "active",
		},
		AuthType: "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Mock failed refresh — 400 indicates definitive credential rejection
	mockSvc.On("Refresh", mock.Anything, connID).Return(&service.RefreshResponse{StatusCode: 400}, errors.New("invalid_grant")).Once()

	// Provider is healthy, so the connection should be expired (not shielded)
	mockHealth.On("GetHealthStatus", providerID).Return("healthy", nil).Once()

	// Should update connection status to expired
	mockRepo.On("UpdateStatus", mock.Anything, connID, "expired").Return(nil).Once()

	// Should update health to expired
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "expired").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockHealth.AssertExpectations(t)
}

func TestConnectionHealthWorker_OAuth2_ProviderDown_ShieldsExpiration(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	providerID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "active",
		},
		ProviderName: "google",
		AuthType:     "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Mock failed refresh — 401 is a definitive credential error, but should be
	// shielded because the provider is unhealthy
	mockSvc.On("Refresh", mock.Anything, connID).Return(&service.RefreshResponse{StatusCode: 401}, errors.New("token revoked")).Once()

	// Provider is unhealthy → should shield the connection from being expired
	mockHealth.On("GetHealthStatus", providerID).Return("unhealthy", nil).Once()

	// Should NOT call UpdateStatus (no expiration)
	// Should update health to "unhealthy" instead of "expired"
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "unhealthy").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockHealth.AssertExpectations(t)
	// Verify UpdateStatus was NOT called — connection should not be expired
	mockRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, connID, "expired")
}

func TestConnectionHealthWorker_APIKey_Expired(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	connID := uuid.New()
	providerID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:         connID,
			ProviderID: providerID,
			Status:     "active",
		},
		AuthType:         "api_key",
		UserInfoEndpoint: server.URL,
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Mock getting token
	creds := map[string]interface{}{"api_key": "secret-key"}
	mockSvc.On("GetToken", mock.Anything, connID).Return(creds, "api_key_strategy", nil).Once()

	// Provider is healthy, so expiration should proceed
	mockHealth.On("GetHealthStatus", providerID).Return("healthy", nil).Once()

	// Should update connection status to expired
	mockRepo.On("UpdateStatus", mock.Anything, connID, "expired").Return(nil).Once()

	// Should update health to expired
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "expired").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockHealth.AssertExpectations(t)
}

func TestConnectionHealthWorker_OAuth2_Upstream5xx_MarksUnhealthy(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// 502 from upstream — transient server error, not a credential issue
	mockSvc.On("Refresh", mock.Anything, connID).Return(&service.RefreshResponse{StatusCode: 502}, errors.New("bad gateway")).Once()

	// Should set health_status to "unhealthy", NOT "expired"
	// Should NOT call UpdateStatus — connection status stays "active"
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "unhealthy").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, connID, "expired")
}

func TestConnectionHealthWorker_OAuth2_403_MarksDegraded(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// 403 — scope issue, credential exists but limited
	mockSvc.On("Refresh", mock.Anything, connID).Return(&service.RefreshResponse{StatusCode: 403}, errors.New("forbidden")).Once()

	// Should set health_status to "degraded", NOT "expired"
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "degraded").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, connID, "expired")
}

func TestConnectionHealthWorker_OAuth2_NetworkError_MarksDegraded(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)
	mockHealth := new(MockProviderHealthLookup)

	connID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "oauth2",
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Nil response — network error, timeout, DNS failure etc.
	mockSvc.On("Refresh", mock.Anything, connID).Return((*service.RefreshResponse)(nil), errors.New("connection refused")).Once()

	// Should set health_status to "degraded" (we don't know if credential is valid)
	done := make(chan struct{})
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "degraded").
		Run(func(args mock.Arguments) { close(done) }).
		Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, mockHealth, 10*time.Millisecond)

	runWorkerUntilSignal(t, worker, done)

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, connID, "expired")
}
