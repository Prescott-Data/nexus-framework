package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
)

// A credential is released only for the subject that owns the connection.

func tokenRequest(connID uuid.UUID, subject string) *http.Request {
	req, _ := http.NewRequest("GET", "/connections/"+connID.String()+"/token", nil)
	if subject != "" {
		req.Header.Set(HeaderSubject, subject)
		req.Header.Set(HeaderAgent, "coder-1")
		req.Header.Set(HeaderOperation, "op-42")
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionID", connID.String())
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func ownedBy(owner string) ConnectionOwner {
	return func(context.Context, uuid.UUID) (string, error) { return owner, nil }
}

func TestSubjectBinding_TokenIsReleasedOnlyForItsOwner(t *testing.T) {
	connID := uuid.New()
	token := map[string]interface{}{"access_token": "at-ada", "credentials": map[string]interface{}{"access_token": "at-ada"}}
	cases := []struct {
		name, mode, subject, owner string
		lookup                     bool
		status                     int
		code                       string
	}{
		{"owner gets it", "optional", "ws-ada", "ws-ada", true, http.StatusOK, ""},
		{"someone else does not", "optional", "ws-bob", "ws-ada", true, http.StatusForbidden, "subject_mismatch"},
		{"no subject, optional: as before", "optional", "", "ws-ada", true, http.StatusOK, ""},
		{"no subject, required: refused", "required", "", "ws-ada", true, http.StatusForbidden, "subject_required"},
		{"owner unknown: refused, not trusted", "optional", "ws-ada", "", false, http.StatusForbidden, "subject_unverifiable"},
		{"binding off", "off", "ws-bob", "ws-ada", true, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockConnectionService)
			if tc.status == http.StatusOK {
				svc.On("GetToken", mock.Anything, connID).Return(token, "github", nil).Once()
			}
			cfg := CallbackHandlerConfig{Service: svc, SubjectBinding: tc.mode}
			if tc.lookup {
				cfg.ConnectionOwner = ownedBy(tc.owner)
			}
			rr := httptest.NewRecorder()
			NewCallbackHandler(cfg).GetToken(rr, tokenRequest(connID, tc.subject))

			assert.Equal(t, tc.status, rr.Code)
			if tc.code != "" {
				assert.Contains(t, rr.Body.String(), tc.code)
				assert.NotContains(t, rr.Body.String(), "at-ada")
			}
			svc.AssertExpectations(t) // a refused credential is never even read
		})
	}
}

func TestSubjectBinding_ALookupFailureIsARefusal(t *testing.T) {
	svc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: svc, SubjectBinding: "required",
		ConnectionOwner: func(context.Context, uuid.UUID) (string, error) { return "", errors.New("db down") },
	})
	rr := httptest.NewRecorder()
	handler.GetToken(rr, tokenRequest(uuid.New(), "ws-ada"))

	assert.Equal(t, http.StatusForbidden, rr.Code)
	svc.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
}

func TestSubjectBinding_ResolveIsForTheSubjectsOwnWorkspace(t *testing.T) {
	svc := new(MockConnectionService)
	svc.On("GetTokenByWorkspaceAndProvider", mock.Anything, "ws-ada", "github").
		Return(map[string]interface{}{"access_token": "at-ada"}, "github", nil).Once()
	handler := NewCallbackHandler(CallbackHandlerConfig{Service: svc, SubjectBinding: "required"})

	for subject, want := range map[string]int{"ws-ada": http.StatusOK, "ws-bob": http.StatusForbidden, "": http.StatusForbidden} {
		req, _ := http.NewRequest("GET", "/connections/resolve?workspace_id=ws-ada&provider_name=github", nil)
		if subject != "" {
			req.Header.Set(HeaderSubject, subject)
		}
		rr := httptest.NewRecorder()
		handler.ResolveToken(rr, req)
		assert.Equal(t, want, rr.Code, "subject %q", subject)
	}
	svc.AssertExpectations(t)
}

func TestSubjectBinding_ARefreshIsReleasedOnlyAsATokenIs(t *testing.T) {
	connID := uuid.New()
	svc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{Service: svc, ConnectionOwner: ownedBy("ws-ada")})
	req, _ := http.NewRequest("POST", "/connections/"+connID.String()+"/refresh", nil)
	req.Header.Set(HeaderSubject, "ws-bob")
	rr := httptest.NewRecorder()

	handler.Refresh(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	svc.AssertNotCalled(t, "Refresh", mock.Anything, mock.Anything)
}

func TestSubjectBinding_AnAgentSessionActsForTheSubjectThatAsked(t *testing.T) {
	body := []byte(`{"agent_id":"crm-agent","connection_id":"` + uuid.New().String() + `","scopes":["crm:read"]}`)

	required := NewAgentsHandler(new(MockAgentService), WithAgentSubjectBinding("required"))
	rr := httptest.NewRecorder()
	required.CreateSession(rr, httptest.NewRequest(http.MethodPost, "/v1/agent-sessions", bytes.NewReader(body)))
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "subject_required")

	svc := new(MockAgentService)
	svc.On("RequestAgentSession", mock.Anything, mock.MatchedBy(func(req service.AgentSessionRequest) bool {
		return req.ActingFor == "ws-ada"
	})).Return(&service.AgentSessionResponse{ActingFor: "ws-ada", OBO: true}, nil).Once()
	req := httptest.NewRequest(http.MethodPost, "/v1/agent-sessions", bytes.NewReader(body))
	req.Header.Set(HeaderSubject, "ws-ada")
	rr = httptest.NewRecorder()
	NewAgentsHandler(svc).CreateSession(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)
	svc.AssertExpectations(t)
}

func TestSubjectBinding_IsExactNotCaseFolded(t *testing.T) {
	connID := uuid.New()
	svc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{Service: svc, ConnectionOwner: ownedBy("ACME")})
	rr := httptest.NewRecorder()
	handler.GetToken(rr, tokenRequest(connID, "acme"))
	assert.Equal(t, http.StatusForbidden, rr.Code)

	req, _ := http.NewRequest("GET", "/connections/resolve?workspace_id=ACME&provider_name=github", nil)
	req.Header.Set(HeaderSubject, "acme")
	rr = httptest.NewRecorder()
	handler.ResolveToken(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
	svc.AssertNotCalled(t, "GetTokenByWorkspaceAndProvider", mock.Anything, mock.Anything, mock.Anything)
}

func TestStatus_ReportsAConnectionWithoutReleasingAnything(t *testing.T) {
	connID := uuid.New()
	svc := new(MockConnectionService)
	handler := NewCallbackHandler(CallbackHandlerConfig{
		Service: svc, SubjectBinding: "required",
		ConnectionStatus: func(context.Context, uuid.UUID) (string, error) { return "active", nil },
	})
	req, _ := http.NewRequest("GET", "/connections/"+connID.String()+"/status", nil)
	rr := httptest.NewRecorder()
	handler.Status(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"active"`)
	assert.NotContains(t, rr.Body.String(), "token")
	svc.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
}
