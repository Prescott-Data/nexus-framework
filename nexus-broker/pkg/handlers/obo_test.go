package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/stretchr/testify/require"
)

type oboHandlerService struct {
	*MockAgentService
	request service.OBOSessionRequest
}

func (s *oboHandlerService) RequestOBOSession(_ context.Context, req service.OBOSessionRequest) (*service.AgentSessionResponse, error) {
	s.request = req
	return &service.AgentSessionResponse{SessionID: "sess_obo", OBO: true, ActingFor: "alice"}, nil
}

func TestCreateOBOSession(t *testing.T) {
	svc := &oboHandlerService{MockAgentService: new(MockAgentService)}
	handler := NewAgentsHandler(svc, WithAgentSubjectBinding("off"))
	req := httptest.NewRequest(http.MethodPost, "/v1/agent-sessions/obo", strings.NewReader(`{"agent_id":"agent","provider_name":"provider","scopes":["read"],"user_context_token":"secret"}`))
	req.Header.Set(HeaderSubject, "alice")
	w := httptest.NewRecorder()
	handler.CreateOBOSession(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, "alice", svc.request.ActingFor)
	require.Equal(t, "secret", svc.request.UserContextToken)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "secret")
}

func TestCreateOBOSessionInvalidJSON(t *testing.T) {
	for _, body := range []string{`{`, `{"tenant_id":"forged"}`, `{} {}`} {
		handler := NewAgentsHandler(new(MockAgentService))
		w := httptest.NewRecorder()
		handler.CreateOBOSession(w, httptest.NewRequest(http.MethodPost, "/v1/agent-sessions/obo", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}
