package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOBOSessionVerifiedBackendFlow(t *testing.T) {
	for _, scenario := range []string{"success", "user_denied", "agent_denied", "wrong_owner", "forged_subject", "forged_workspace", "unknown_scopes", "backend_rejected", "backend_failed", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/auth/verify-agent-token", r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)
				var body map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "private-user-token", body["token"])
				switch scenario {
				case "backend_rejected":
					w.WriteHeader(http.StatusUnauthorized)
					return
				case "backend_failed":
					w.WriteHeader(http.StatusInternalServerError)
					return
				case "malformed":
					_, _ = w.Write([]byte(`{"user_id":`))
					return
				}
				permissions := []string{"read"}
				if scenario == "user_denied" {
					permissions = nil
				}
				_ = json.NewEncoder(w).Encode(service.VerifiedUserContext{UserID: "alice", TenantID: "org-a", ClearanceLevel: 2, Permissions: permissions})
			}))
			defer backend.Close()
			agents := new(MockAgentRepository)
			connections := new(MockConnectionRepository)
			tokens := new(MockAgentConnectionService)
			svc := service.NewAgentService(agents, connections, tokens, service.WithUserContextVerifier(service.NewBackendUserContextVerifier(backend.URL)))
			id := uuid.New()
			req := service.OBOSessionRequest{AgentSessionRequest: service.AgentSessionRequest{AgentID: "agent", ConnectionID: id.String(), Scopes: []string{"read"}}, UserContextToken: "private-user-token"}
			if scenario == "forged_subject" {
				req.ActingFor = "bob"
			}
			if scenario == "forged_workspace" {
				req.WorkspaceID = "bob"
			}
			if scenario == "success" || scenario == "agent_denied" || scenario == "wrong_owner" || scenario == "unknown_scopes" {
				allowed := []string{"read"}
				if scenario == "agent_denied" {
					allowed = nil
				}
				agents.On("GetAgent", mock.Anything, "agent").Return(&domain.Agent{ID: "agent", Active: true, AllowedScopes: allowed}, nil).Once()
				if scenario != "agent_denied" {
					owner := "alice"
					if scenario == "wrong_owner" {
						owner = "bob"
					}
					scopes := []string{"read"}
					if scenario == "unknown_scopes" {
						scopes = nil
					}
					connections.On("GetWithProvider", mock.Anything, id).Return(&domain.ConnectionWithProvider{Connection: domain.Connection{ID: id, WorkspaceID: owner, Status: "active", Scopes: scopes}, AuthType: "oauth2"}, nil).Once()
				}
			}
			if scenario == "success" {
				tokens.On("GetToken", mock.Anything, id).Return(map[string]interface{}{"access_token": "provider-token"}, "provider", nil).Once()
				agents.On("CreateSession", mock.Anything, mock.MatchedBy(func(s *domain.AgentSession) bool {
					return s.OBO && s.ActingFor == "alice" && s.TenantID == "org-a" && s.ClearanceLevel == 2 && s.ExpiresAt.After(time.Now())
				})).Return(nil).Once()
			}
			resp, err := svc.(service.OBOSessionService).RequestOBOSession(context.Background(), req)
			if scenario == "success" {
				require.NoError(t, err)
				require.True(t, resp.OBO)
				require.Equal(t, "alice", resp.ActingFor)
				require.Equal(t, "org-a", resp.TenantID)
				require.Equal(t, 2, resp.ClearanceLevel)
			} else {
				require.Error(t, err)
				require.Nil(t, resp)
				tokens.AssertNotCalled(t, "GetToken", mock.Anything, mock.Anything)
				agents.AssertNotCalled(t, "CreateSession", mock.Anything, mock.Anything)
			}
			agents.AssertExpectations(t)
			connections.AssertExpectations(t)
			tokens.AssertExpectations(t)
		})
	}
}

func TestOBODisabledAndMissingToken(t *testing.T) {
	svc := service.NewAgentService(nil, nil, nil)
	_, err := svc.(service.OBOSessionService).RequestOBOSession(context.Background(), service.OBOSessionRequest{})
	require.ErrorContains(t, err, "not configured")
	svc = service.NewAgentService(nil, nil, nil, service.WithUserContextVerifier(service.NewBackendUserContextVerifier("https://example.com")))
	_, err = svc.(service.OBOSessionService).RequestOBOSession(context.Background(), service.OBOSessionRequest{})
	require.ErrorContains(t, err, "user_context_token is required")
}

func TestIdentityVerifierDoesNotFollowRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("credential leaked through redirect") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := service.NewBackendUserContextVerifier(redirect.URL).Verify(context.Background(), "secret")
	require.Error(t, err)
}

func TestIdentityVerifierCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.NewBackendUserContextVerifier("https://example.com").Verify(ctx, "secret")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
}

func TestIdentityVerifierRejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"user_id":"alice","clearance_level":-1}`, `{"user_id":"alice","permissions":"read"}`, `{"user_id":"alice"} {}`, strings.Repeat("x", (1<<20)+1)} {
		t.Run("invalid", func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer backend.Close()
			_, err := service.NewBackendUserContextVerifier(backend.URL).Verify(context.Background(), "secret")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
