package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
)

func TestConnectionsList_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	now := time.Now()
	expected := []domain.ConnectionSummary{
		{
			ID:           uuid.New(),
			ProviderID:   uuid.New(),
			ProviderName: "google",
			AuthType:     "oauth2",
			Status:       "active",
			Scopes:       []string{"email", "calendar.read"},
			HealthStatus: "healthy",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           uuid.New(),
			ProviderID:   uuid.New(),
			ProviderName: "stripe",
			AuthType:     "api_key",
			Status:       "active",
			HealthStatus: "unhealthy",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}

	mockSvc.On("ListConnections", mock.Anything, "ws-123").Return(expected, nil).Once()

	req := httptest.NewRequest("GET", "/connections?workspace_id=ws-123", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "google")
	assert.Contains(t, rr.Body.String(), "stripe")
	assert.Contains(t, rr.Body.String(), `"health_status":"healthy"`)
	assert.Contains(t, rr.Body.String(), `"health_status":"unhealthy"`)
	mockSvc.AssertExpectations(t)
}

func TestConnectionsList_EmptyResult(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	mockSvc.On("ListConnections", mock.Anything, "ws-empty").Return([]domain.ConnectionSummary{}, nil).Once()

	req := httptest.NewRequest("GET", "/connections?workspace_id=ws-empty", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "[]", rr.Body.String())
	mockSvc.AssertExpectations(t)
}

func TestConnectionsList_MissingWorkspaceID(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	req := httptest.NewRequest("GET", "/connections", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing_workspace_id")
	// Service should never be called
	mockSvc.AssertNotCalled(t, "ListConnections")
}

func TestConnectionsList_ServiceError(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	mockSvc.On("ListConnections", mock.Anything, "ws-broken").Return(nil, errors.New("database unreachable")).Once()

	req := httptest.NewRequest("GET", "/connections?workspace_id=ws-broken", nil)
	rr := httptest.NewRecorder()

	handler.List(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	mockSvc.AssertExpectations(t)
}
