package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
)

// routeRevoke exercises the handler through a chi router so the {connectionID}
// URL param is populated the same way it is in production.
func routeRevoke(handler *ConnectionsHandler, req *http.Request) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Delete("/connections/{connectionID}", handler.Revoke)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestConnectionsRevoke_Success(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	mockSvc.On("RevokeConnection", mock.Anything, service.RevokeRequest{
		ConnectionID: connID,
		WorkspaceID:  "ws-123",
		Reason:       "user disconnected",
	}).Return(&service.RevokeResult{
		ConnectionID:    connID,
		ProviderName:    "google",
		Status:          service.StatusRevoked,
		RevokedAt:       time.Now().UTC(),
		TokenDeleted:    true,
		ProviderRevoked: true,
		SessionsClosed:  2,
	}, nil).Once()

	body := strings.NewReader(`{"reason":"user disconnected","workspace_id":"ws-123"}`)
	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String(), body)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"revoked"`)
	assert.Contains(t, rr.Body.String(), `"provider_revoked":true`)
	assert.Contains(t, rr.Body.String(), `"sessions_closed":2`)
	mockSvc.AssertExpectations(t)
}

// A bare DELETE with no body must work: the reason and workspace are optional.
func TestConnectionsRevoke_NoBody(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	mockSvc.On("RevokeConnection", mock.Anything, service.RevokeRequest{
		ConnectionID: connID,
	}).Return(&service.RevokeResult{
		ConnectionID: connID,
		Status:       service.StatusRevoked,
		TokenDeleted: true,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String(), nil)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

// Query parameters are an alternative to the body for clients that cannot send
// one on a DELETE.
func TestConnectionsRevoke_QueryParams(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	mockSvc.On("RevokeConnection", mock.Anything, service.RevokeRequest{
		ConnectionID: connID,
		WorkspaceID:  "ws-q",
		Reason:       "offboarding",
	}).Return(&service.RevokeResult{ConnectionID: connID, Status: service.StatusRevoked}, nil).Once()

	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String()+"?workspace_id=ws-q&reason=offboarding", nil)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockSvc.AssertExpectations(t)
}

func TestConnectionsRevoke_InvalidUUID(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	req := httptest.NewRequest(http.MethodDelete, "/connections/not-a-uuid", nil)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid_connection_id")
	mockSvc.AssertNotCalled(t, "RevokeConnection")
}

func TestConnectionsRevoke_NotFoundPropagatesStatus(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	mockSvc.On("RevokeConnection", mock.Anything, mock.Anything).
		Return(nil, service.ErrNotFound("connection_not_found", "Connection not found")).Once()

	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String(), nil)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "connection_not_found")
	mockSvc.AssertExpectations(t)
}

func TestConnectionsRevoke_MalformedBody(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String(), strings.NewReader(`{"reason":`))
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid_json")
	mockSvc.AssertNotCalled(t, "RevokeConnection")
}

// errReader fails partway through, simulating a truncated or aborted upload.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// A body that cannot be read must not be treated as an absent body: it may have
// carried workspace_id, and dropping it would turn a scoped revocation into an
// unscoped one against a destructive endpoint.
func TestConnectionsRevoke_UnreadableBodyIsRejected(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/connections/"+connID.String(), errReader{})
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid_body")
	mockSvc.AssertNotCalled(t, "RevokeConnection", mock.Anything, mock.Anything)
}

// The body is bounded by maxRevokeBody, but a query-string reason is not, and it
// lands in a TEXT column and the audit log.
func TestConnectionsRevoke_OverlongReasonIsRejected(t *testing.T) {
	mockSvc := new(MockConnectionService)
	handler := NewConnectionsHandler(mockSvc)

	connID := uuid.New()
	longReason := strings.Repeat("a", maxRevokeReasonLen+1)
	req := httptest.NewRequest(http.MethodDelete,
		"/connections/"+connID.String()+"?reason="+longReason, nil)
	rr := routeRevoke(handler, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "reason_too_long")
	mockSvc.AssertNotCalled(t, "RevokeConnection", mock.Anything, mock.Anything)
}
