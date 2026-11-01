package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// VerifiedUserContext is supplied by the operator's trusted identity backend.
type VerifiedUserContext struct {
	UserID         string   `json:"user_id"`
	TenantID       string   `json:"tenant_id"`
	ClearanceLevel int      `json:"clearance_level"`
	Permissions    []string `json:"permissions"`
}

type UserContextVerifier interface {
	Verify(context.Context, string) (*VerifiedUserContext, error)
}

type OBOSessionRequest struct {
	AgentSessionRequest
	UserContextToken string `json:"user_context_token"`
}

// OBOSessionService is separate so existing AgentService clients remain compatible.
type OBOSessionService interface {
	RequestOBOSession(context.Context, OBOSessionRequest) (*AgentSessionResponse, error)
}

type AgentServiceOption func(*agentService)

func WithUserContextVerifier(verifier UserContextVerifier) AgentServiceOption {
	return func(s *agentService) { s.oboVerifier = verifier }
}

func (s *agentService) RequestOBOSession(ctx context.Context, req OBOSessionRequest) (*AgentSessionResponse, error) {
	if s.oboVerifier == nil {
		return nil, NewServiceError(http.StatusServiceUnavailable, nil, "obo_disabled", "OBO is not configured")
	}
	if strings.TrimSpace(req.UserContextToken) == "" {
		return nil, ErrBadRequest("missing_user_context_token", "user_context_token is required")
	}
	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return nil, ErrBadRequest("missing_scopes", "at least one scope is required")
	}
	user, err := s.oboVerifier.Verify(ctx, req.UserContextToken)
	if err != nil {
		return nil, err
	}
	if user == nil || strings.TrimSpace(user.UserID) == "" || user.ClearanceLevel < 0 {
		return nil, ErrBadGatewayWithErr(nil, "invalid_user_context", "Identity backend returned invalid user context")
	}
	if firstMissingScope(scopes, user.Permissions) != "" {
		return nil, ErrForbidden("user_scope_not_allowed", "User is not authorized for the requested scopes")
	}
	if req.ActingFor != "" && req.ActingFor != user.UserID {
		return nil, ErrForbidden("subject_mismatch", "acting_for does not match the verified user")
	}
	if req.WorkspaceID != "" && req.WorkspaceID != user.UserID {
		return nil, ErrForbidden("subject_mismatch", "workspace_id does not match the verified user")
	}
	req.ActingFor = user.UserID
	req.WorkspaceID = user.UserID
	req.Scopes = scopes
	return s.requestAgentSession(ctx, req.AgentSessionRequest, user)
}

type backendUserContextVerifier struct {
	endpoint string
	client   *http.Client
}

// NewBackendUserContextVerifier uses a dedicated uncached client. Redirects are
// forbidden because the request contains the user's bearer credential.
func NewBackendUserContextVerifier(baseURL string) UserContextVerifier {
	return &backendUserContextVerifier{
		endpoint: strings.TrimRight(baseURL, "/") + "/auth/verify-agent-token",
		client:   &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (v *backendUserContextVerifier) Verify(ctx context.Context, token string) (*VerifiedUserContext, error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return nil, ErrInternal("verification_failed", "Failed to prepare identity verification")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInternal("verification_failed", "Failed to prepare identity verification")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, ErrBadGatewayWithErr(nil, "identity_backend_unavailable", "Identity backend is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrForbidden("invalid_user_context_token", "User context token was rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ErrBadGatewayWithErr(nil, "identity_backend_failed", "Identity backend verification failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, ErrBadGatewayWithErr(nil, "invalid_user_context", "Identity backend returned invalid user context")
	}
	var user VerifiedUserContext
	if err := json.Unmarshal(data, &user); err != nil || strings.TrimSpace(user.UserID) == "" || user.ClearanceLevel < 0 {
		return nil, ErrBadGatewayWithErr(nil, "invalid_user_context", "Identity backend returned invalid user context")
	}
	return &user, nil
}
