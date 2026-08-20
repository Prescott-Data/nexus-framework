package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockAgentRepository struct {
	mock.Mock
}

func (m *MockAgentRepository) CreateAgent(ctx context.Context, agent *domain.Agent) error {
	args := m.Called(ctx, agent)
	return args.Error(0)
}

func (m *MockAgentRepository) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.Agent), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentRepository) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	args := m.Called(ctx)
	if args.Get(0) != nil {
		return args.Get(0).([]domain.Agent), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentRepository) CreateSession(ctx context.Context, session *domain.AgentSession) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *MockAgentRepository) GetSession(ctx context.Context, sessionID string) (*domain.AgentSession, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.AgentSession), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentRepository) CloseSession(ctx context.Context, sessionID string, closedAt time.Time) error {
	args := m.Called(ctx, sessionID, closedAt)
	return args.Error(0)
}

type MockAgentConnectionService struct {
	mock.Mock
}

func (m *MockAgentConnectionService) CreateConsentSpec(ctx context.Context, req service.CreateConsentRequest) (*service.ConsentSpecResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*service.ConsentSpecResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentConnectionService) ExchangeCodeForTokens(ctx context.Context, state, code, errorParam, errorDesc string) (string, bool, error) {
	args := m.Called(ctx, state, code, errorParam, errorDesc)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *MockAgentConnectionService) GetToken(ctx context.Context, connectionID uuid.UUID) (map[string]interface{}, string, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(map[string]interface{}), args.String(1), args.Error(2)
	}
	return nil, args.String(1), args.Error(2)
}

func (m *MockAgentConnectionService) GetTokenByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (map[string]interface{}, string, error) {
	args := m.Called(ctx, workspaceID, providerName)
	if args.Get(0) != nil {
		return args.Get(0).(map[string]interface{}), args.String(1), args.Error(2)
	}
	return nil, args.String(1), args.Error(2)
}

func (m *MockAgentConnectionService) GetCaptureSchema(ctx context.Context, state string) (string, json.RawMessage, error) {
	args := m.Called(ctx, state)
	if args.Get(1) != nil {
		return args.String(0), args.Get(1).(json.RawMessage), args.Error(2)
	}
	return args.String(0), nil, args.Error(2)
}

func (m *MockAgentConnectionService) SaveCredential(ctx context.Context, state string, credentials map[string]interface{}) (string, error) {
	args := m.Called(ctx, state, credentials)
	return args.String(0), args.Error(1)
}

func (m *MockAgentConnectionService) Refresh(ctx context.Context, connectionID uuid.UUID) (*service.RefreshResponse, error) {
	args := m.Called(ctx, connectionID)
	if args.Get(0) != nil {
		return args.Get(0).(*service.RefreshResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentConnectionService) ListConnections(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error) {
	args := m.Called(ctx, workspaceID)
	if args.Get(0) != nil {
		return args.Get(0).([]domain.ConnectionSummary), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentConnectionService) ExchangeSAMLResponse(ctx context.Context, r *http.Request) (string, error) {
	args := m.Called(ctx, r)
	return args.String(0), args.Error(1)
}

func (m *MockAgentConnectionService) GetSAMLMetadata(ctx context.Context, providerID uuid.UUID) ([]byte, error) {
	args := m.Called(ctx, providerID)
	if args.Get(0) != nil {
		return args.Get(0).([]byte), args.Error(1)
	}
	return nil, args.Error(1)
}

func setupAgentService() (*MockAgentRepository, *MockConnectionRepository, *MockAgentConnectionService, service.AgentService) {
	agentRepo := new(MockAgentRepository)
	connRepo := new(MockConnectionRepository)
	connSvc := new(MockAgentConnectionService)
	return agentRepo, connRepo, connSvc, service.NewAgentService(agentRepo, connRepo, connSvc)
}

func TestAgentService_RegisterAgent(t *testing.T) {
	agentRepo, _, _, svc := setupAgentService()

	agentRepo.On("GetAgent", mock.Anything, "crm-agent").Return(nil, sql.ErrNoRows).Once()
	agentRepo.On("CreateAgent", mock.Anything, mock.MatchedBy(func(agent *domain.Agent) bool {
		return agent.ID == "crm-agent" &&
			agent.Description == "CRM reader" &&
			agent.Active &&
			assert.ElementsMatch(t, []string{"crm:contacts:read", "crm:contacts:write"}, agent.AllowedScopes)
	})).Return(nil).Once()

	agent, err := svc.RegisterAgent(context.Background(), service.RegisterAgentRequest{
		AgentID:       " crm-agent ",
		Description:   " CRM reader ",
		AllowedScopes: []string{"crm:contacts:read", "crm:contacts:read", "crm:contacts:write"},
	})

	assert.NoError(t, err)
	assert.Equal(t, "crm-agent", agent.ID)
	assert.Equal(t, []string{"crm:contacts:read", "crm:contacts:write"}, agent.AllowedScopes)
	agentRepo.AssertExpectations(t)
}

func TestAgentService_RequestAgentSession_Success(t *testing.T) {
	agentRepo, connRepo, connSvc, svc := setupAgentService()
	connID := uuid.New()

	agentRepo.On("GetAgent", mock.Anything, "crm-agent").Return(&domain.Agent{
		ID:            "crm-agent",
		AllowedScopes: []string{"crm:contacts:read", "crm:contacts:write"},
		Active:        true,
	}, nil).Once()
	connRepo.On("GetActiveByWorkspaceAndProvider", mock.Anything, "ws-123", "salesforce").Return(&domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
			Scopes: []string{"crm:contacts:read", "crm:contacts:write"},
		},
		ProviderName: "salesforce",
		AuthType:     "oauth2",
	}, nil).Once()
	connSvc.On("GetToken", mock.Anything, connID).Return(map[string]interface{}{
		"access_token":  "scoped-access-token",
		"refresh_token": "must-not-be-returned",
	}, "salesforce", nil).Once()
	agentRepo.On("CreateSession", mock.Anything, mock.MatchedBy(func(session *domain.AgentSession) bool {
		return session.AgentID == "crm-agent" &&
			session.ConnectionID == connID &&
			stringsHasPrefix(session.SessionID, "sess_") &&
			assert.ElementsMatch(t, []string{"crm:contacts:read"}, session.ScopesGranted) &&
			session.ExpiresAt.After(time.Now()) &&
			!session.OBO &&
			session.ClearanceLevel == 1
	})).Return(nil).Once()

	resp, err := svc.RequestAgentSession(context.Background(), service.AgentSessionRequest{
		AgentID:      "crm-agent",
		WorkspaceID:  "ws-123",
		ProviderName: "salesforce",
		Scopes:       []string{"crm:contacts:read"},
		TTLSeconds:   900,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "bearer", resp.TokenType)
	assert.Equal(t, "scoped-access-token", resp.AccessToken)
	assert.True(t, resp.Active)
	assert.Equal(t, connID, resp.ConnectionID)
	agentRepo.AssertExpectations(t)
	connRepo.AssertExpectations(t)
	connSvc.AssertExpectations(t)
}

func TestAgentService_RequestAgentSession_RejectsUnallowedScope(t *testing.T) {
	agentRepo, connRepo, connSvc, svc := setupAgentService()

	agentRepo.On("GetAgent", mock.Anything, "crm-agent").Return(&domain.Agent{
		ID:            "crm-agent",
		AllowedScopes: []string{"crm:contacts:read"},
		Active:        true,
	}, nil).Once()

	resp, err := svc.RequestAgentSession(context.Background(), service.AgentSessionRequest{
		AgentID:      "crm-agent",
		WorkspaceID:  "ws-123",
		ProviderName: "salesforce",
		Scopes:       []string{"crm:contacts:delete"},
	})

	assert.Nil(t, resp)
	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "scope_not_allowed", svcErr.Code)
	assert.Equal(t, 403, svcErr.HTTPStatus)
	connRepo.AssertNotCalled(t, "GetActiveByWorkspaceAndProvider", mock.Anything, mock.Anything, mock.Anything)
	connSvc.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
}

func TestAgentService_RequestAgentSession_RejectsTTLAboveMax(t *testing.T) {
	_, connRepo, connSvc, svc := setupAgentService()

	resp, err := svc.RequestAgentSession(context.Background(), service.AgentSessionRequest{
		AgentID:      "crm-agent",
		WorkspaceID:  "ws-123",
		ProviderName: "salesforce",
		Scopes:       []string{"crm:contacts:read"},
		TTLSeconds:   3601,
	})

	assert.Nil(t, resp)
	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "invalid_ttl", svcErr.Code)
	assert.Equal(t, 400, svcErr.HTTPStatus)
	connRepo.AssertNotCalled(t, "GetActiveByWorkspaceAndProvider", mock.Anything, mock.Anything, mock.Anything)
	connSvc.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
}

func TestAgentService_RequestAgentSession_RejectsConnectionScopeGap(t *testing.T) {
	agentRepo, connRepo, connSvc, svc := setupAgentService()
	connID := uuid.New()

	agentRepo.On("GetAgent", mock.Anything, "crm-agent").Return(&domain.Agent{
		ID:            "crm-agent",
		AllowedScopes: []string{"crm:contacts:delete"},
		Active:        true,
	}, nil).Once()
	connRepo.On("GetActiveByWorkspaceAndProvider", mock.Anything, "ws-123", "salesforce").Return(&domain.ConnectionWithProvider{
		Connection: domain.Connection{
			ID:     connID,
			Status: "active",
			Scopes: []string{"crm:contacts:read"},
		},
		ProviderName: "salesforce",
		AuthType:     "oauth2",
	}, nil).Once()

	resp, err := svc.RequestAgentSession(context.Background(), service.AgentSessionRequest{
		AgentID:      "crm-agent",
		WorkspaceID:  "ws-123",
		ProviderName: "salesforce",
		Scopes:       []string{"crm:contacts:delete"},
	})

	assert.Nil(t, resp)
	var svcErr *service.ServiceError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "scope_not_granted", svcErr.Code)
	assert.Equal(t, 403, svcErr.HTTPStatus)
	connSvc.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
}

func TestAgentService_GetAgentSession_InactiveWhenExpiredOrClosed(t *testing.T) {
	agentRepo, _, _, svc := setupAgentService()
	closedAt := time.Now().Add(-time.Minute)
	agentRepo.On("GetSession", mock.Anything, "sess_closed").Return(&domain.AgentSession{
		SessionID:      "sess_closed",
		AgentID:        "crm-agent",
		ConnectionID:   uuid.New(),
		ScopesGranted:  []string{"crm:contacts:read"},
		ExpiresAt:      time.Now().Add(time.Hour),
		ClosedAt:       &closedAt,
		ClearanceLevel: 1,
	}, nil).Once()

	resp, err := svc.GetAgentSession(context.Background(), "sess_closed")

	assert.NoError(t, err)
	assert.False(t, resp.Active)
	agentRepo.AssertExpectations(t)
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
