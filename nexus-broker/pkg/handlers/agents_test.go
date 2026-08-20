package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockAgentService struct {
	mock.Mock
}

func (m *MockAgentService) RegisterAgent(ctx context.Context, req service.RegisterAgentRequest) (*domain.Agent, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*domain.Agent), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentService) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	args := m.Called(ctx)
	if args.Get(0) != nil {
		return args.Get(0).([]domain.Agent), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentService) RequestAgentSession(ctx context.Context, req service.AgentSessionRequest) (*service.AgentSessionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*service.AgentSessionResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentService) GetAgentSession(ctx context.Context, sessionID string) (*service.AgentSessionResponse, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) != nil {
		return args.Get(0).(*service.AgentSessionResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockAgentService) CloseAgentSession(ctx context.Context, sessionID string) (*service.AgentSessionResponse, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) != nil {
		return args.Get(0).(*service.AgentSessionResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func TestAgentsHandler_Register(t *testing.T) {
	mockSvc := new(MockAgentService)
	handler := NewAgentsHandler(mockSvc)

	body := []byte(`{"agent_id":"crm-agent","description":"CRM","allowed_scopes":["crm:contacts:read"]}`)
	expectedReq := service.RegisterAgentRequest{
		AgentID:       "crm-agent",
		Description:   "CRM",
		AllowedScopes: []string{"crm:contacts:read"},
	}
	mockSvc.On("RegisterAgent", mock.Anything, expectedReq).Return(&domain.Agent{
		ID:            "crm-agent",
		Description:   "CRM",
		AllowedScopes: []string{"crm:contacts:read"},
		CreatedAt:     time.Now(),
		Active:        true,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.Register(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Contains(t, rr.Body.String(), `"agent_id":"crm-agent"`)
	mockSvc.AssertExpectations(t)
}

func TestAgentsHandler_CreateSessionScopeDenied(t *testing.T) {
	mockSvc := new(MockAgentService)
	handler := NewAgentsHandler(mockSvc)

	body := []byte(`{"agent_id":"crm-agent","workspace_id":"ws-123","provider_name":"salesforce","scopes":["crm:contacts:delete"]}`)
	expectedReq := service.AgentSessionRequest{
		AgentID:      "crm-agent",
		WorkspaceID:  "ws-123",
		ProviderName: "salesforce",
		Scopes:       []string{"crm:contacts:delete"},
	}
	mockSvc.On("RequestAgentSession", mock.Anything, expectedReq).Return(nil,
		service.ErrForbidden("scope_not_allowed", "scope is not allowed")).Once()

	req := httptest.NewRequest(http.MethodPost, "/v1/agent-sessions", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.CreateSession(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "scope_not_allowed")
	mockSvc.AssertExpectations(t)
}

func TestAgentsHandler_CloseSession(t *testing.T) {
	mockSvc := new(MockAgentService)
	handler := NewAgentsHandler(mockSvc)

	connID := uuid.New()
	mockSvc.On("CloseAgentSession", mock.Anything, "sess_abc").Return(&service.AgentSessionResponse{
		SessionID:      "sess_abc",
		AgentID:        "crm-agent",
		ConnectionID:   connID,
		ScopesGranted:  []string{"crm:contacts:read"},
		ExpiresAt:      time.Now().Add(time.Minute),
		Active:         false,
		ClearanceLevel: 1,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodDelete, "/v1/agent-sessions/sess_abc", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess_abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.CloseSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"closed"`)
	assert.Contains(t, rr.Body.String(), `"active":false`)
	mockSvc.AssertExpectations(t)
}

func TestAgentsHandler_ListUsesWrapper(t *testing.T) {
	mockSvc := new(MockAgentService)
	handler := NewAgentsHandler(mockSvc)
	mockSvc.On("ListAgents", mock.Anything).Return([]domain.Agent{{ID: "crm-agent", Active: true}}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/agents", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var payload map[string][]domain.Agent
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &payload))
	assert.Len(t, payload["agents"], 1)
	mockSvc.AssertExpectations(t)
}
