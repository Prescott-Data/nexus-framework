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

func TestConnectionHealthWorker_OAuth2_Healthy(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)

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
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "healthy").Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, 10*time.Millisecond)
	
	ctx, cancel := context.WithCancel(context.Background())
	go worker.Start(ctx)
	
	time.Sleep(50 * time.Millisecond) // Give it time to run at least once
	cancel()

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestConnectionHealthWorker_OAuth2_Expired(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)

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

	// Mock failed refresh
	mockSvc.On("Refresh", mock.Anything, connID).Return((*service.RefreshResponse)(nil), errors.New("invalid_grant")).Once()

	// Should update connection status to expired
	mockRepo.On("UpdateStatus", mock.Anything, connID, "expired").Return(nil).Once()

	// Should update health to expired
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "expired").Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, 10*time.Millisecond)
	
	ctx, cancel := context.WithCancel(context.Background())
	go worker.Start(ctx)
	
	time.Sleep(50 * time.Millisecond)
	cancel()

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestConnectionHealthWorker_APIKey_Expired(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSvc := new(MockConnectionService)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	connID := uuid.New()
	conn := &domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
		},
		AuthType: "api_key",
		UserInfoEndpoint: server.URL,
	}

	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{conn}, nil).Once()
	mockRepo.On("GetForHealthCheck", mock.Anything, 100).Return([]*domain.ConnectionWithProvider{}, nil)

	// Mock getting token
	creds := map[string]interface{}{"api_key": "secret-key"}
	mockSvc.On("GetToken", mock.Anything, connID).Return(creds, "api_key_strategy", nil).Once()

	// Should update connection status to expired
	mockRepo.On("UpdateStatus", mock.Anything, connID, "expired").Return(nil).Once()

	// Should update health to expired
	mockRepo.On("UpdateHealthStatus", mock.Anything, connID, "expired").Return(nil).Once()

	worker := service.NewConnectionHealthWorker(mockRepo, mockSvc, 10*time.Millisecond)
	
	ctx, cancel := context.WithCancel(context.Background())
	go worker.Start(ctx)
	
	time.Sleep(50 * time.Millisecond)
	cancel()

	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}
