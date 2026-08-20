package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/google/uuid"
)

const (
	defaultAgentSessionTTL = 15 * time.Minute
	maxAgentSessionTTL     = time.Hour
)

var validAgentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// AgentService handles agent registration and scoped session issuance.
type AgentService interface {
	RegisterAgent(ctx context.Context, req RegisterAgentRequest) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	RequestAgentSession(ctx context.Context, req AgentSessionRequest) (*AgentSessionResponse, error)
	GetAgentSession(ctx context.Context, sessionID string) (*AgentSessionResponse, error)
	CloseAgentSession(ctx context.Context, sessionID string) (*AgentSessionResponse, error)
}

type agentService struct {
	agentRepo repository.AgentRepository
	connRepo  repository.ConnectionRepository
	connSvc   ConnectionService
	now       func() time.Time
}

type RegisterAgentRequest struct {
	AgentID       string   `json:"agent_id"`
	Description   string   `json:"description"`
	AllowedScopes []string `json:"allowed_scopes"`
}

type AgentSessionRequest struct {
	AgentID      string   `json:"agent_id"`
	WorkspaceID  string   `json:"workspace_id,omitempty"`
	ProviderName string   `json:"provider_name,omitempty"`
	ConnectionID string   `json:"connection_id,omitempty"`
	Scopes       []string `json:"scopes"`
	TTLSeconds   int      `json:"ttl_seconds,omitempty"`
}

type AgentSessionResponse struct {
	SessionID      string    `json:"session_id"`
	AgentID        string    `json:"agent_id"`
	ConnectionID   uuid.UUID `json:"connection_id"`
	ScopesGranted  []string  `json:"scopes_granted"`
	ExpiresAt      time.Time `json:"expires_at"`
	Active         bool      `json:"active"`
	TokenType      string    `json:"token_type,omitempty"`
	AccessToken    string    `json:"access_token,omitempty"`
	OBO            bool      `json:"obo"`
	ActingFor      string    `json:"acting_for,omitempty"`
	TenantID       string    `json:"tenant_id,omitempty"`
	ClearanceLevel int       `json:"clearance_level"`
}

func NewAgentService(agentRepo repository.AgentRepository, connRepo repository.ConnectionRepository, connSvc ConnectionService) AgentService {
	return newAgentService(agentRepo, connRepo, connSvc, time.Now)
}

func newAgentService(agentRepo repository.AgentRepository, connRepo repository.ConnectionRepository, connSvc ConnectionService, now func() time.Time) AgentService {
	return &agentService{
		agentRepo: agentRepo,
		connRepo:  connRepo,
		connSvc:   connSvc,
		now:       now,
	}
}

func (s *agentService) RegisterAgent(ctx context.Context, req RegisterAgentRequest) (*domain.Agent, error) {
	agentID := strings.TrimSpace(req.AgentID)
	if !validAgentID(agentID) {
		return nil, ErrBadRequest("invalid_agent_id", "agent_id must start with a letter or number and contain only letters, numbers, dots, underscores, colons, or hyphens")
	}

	allowedScopes, err := normalizeScopes(req.AllowedScopes)
	if err != nil {
		return nil, err
	}

	if _, err := s.agentRepo.GetAgent(ctx, agentID); err == nil {
		return nil, ErrConflict("agent_exists", "Agent already exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInternalWithErr(err, "agent_lookup_failed", "Failed to check agent registry")
	}

	agent := &domain.Agent{
		ID:            agentID,
		Description:   strings.TrimSpace(req.Description),
		AllowedScopes: allowedScopes,
		Active:        true,
	}
	if err := s.agentRepo.CreateAgent(ctx, agent); err != nil {
		return nil, ErrInternalWithErr(err, "agent_create_failed", "Failed to register agent")
	}
	return agent, nil
}

func (s *agentService) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	agents, err := s.agentRepo.ListAgents(ctx)
	if err != nil {
		return nil, ErrInternalWithErr(err, "agent_list_failed", "Failed to list agents")
	}
	if agents == nil {
		agents = []domain.Agent{}
	}
	return agents, nil
}

func (s *agentService) RequestAgentSession(ctx context.Context, req AgentSessionRequest) (*AgentSessionResponse, error) {
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return nil, ErrBadRequest("missing_agent_id", "agent_id is required")
	}

	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return nil, ErrBadRequest("missing_scopes", "at least one scope is required")
	}

	ttl, err := sessionTTL(req.TTLSeconds)
	if err != nil {
		return nil, err
	}

	agent, err := s.agentRepo.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound("agent_not_found", "Agent not found")
		}
		return nil, ErrInternalWithErr(err, "agent_lookup_failed", "Failed to load agent")
	}
	if !agent.Active {
		return nil, ErrForbidden("agent_inactive", "Agent is inactive")
	}
	if missing := firstMissingScope(scopes, agent.AllowedScopes); missing != "" {
		return nil, ErrForbidden("scope_not_allowed", fmt.Sprintf("scope %q is not allowed for agent %s", missing, agent.ID))
	}

	conn, err := s.resolveSessionConnection(ctx, req)
	if err != nil {
		return nil, err
	}
	if conn.AuthType != "" && conn.AuthType != "oauth2" {
		return nil, ErrBadRequest("unsupported_agent_session_provider", "agent sessions currently require an OAuth2 connection")
	}
	if missing := firstConnectionMissingScope(scopes, conn.Scopes); missing != "" {
		return nil, ErrForbidden("scope_not_granted", fmt.Sprintf("scope %q was not granted on the underlying connection", missing))
	}

	tokenResponse, _, err := s.connSvc.GetToken(ctx, conn.ID)
	if err != nil {
		return nil, err
	}
	accessToken := extractAccessToken(tokenResponse)
	if accessToken == "" {
		return nil, ErrBadRequest("access_token_unavailable", "OAuth access token is unavailable for this connection")
	}

	now := s.now().UTC()
	session := &domain.AgentSession{
		SessionID:      newAgentSessionID(),
		AgentID:        agent.ID,
		ConnectionID:   conn.ID,
		ScopesGranted:  scopes,
		ExpiresAt:      now.Add(ttl),
		OBO:            false,
		ClearanceLevel: 1,
	}
	if err := s.agentRepo.CreateSession(ctx, session); err != nil {
		return nil, ErrInternalWithErr(err, "session_create_failed", "Failed to create agent session")
	}

	resp := agentSessionResponse(session, now)
	resp.TokenType = "bearer"
	resp.AccessToken = accessToken
	return resp, nil
}

func (s *agentService) GetAgentSession(ctx context.Context, sessionID string) (*AgentSessionResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrBadRequest("missing_session_id", "session_id is required")
	}

	session, err := s.agentRepo.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound("session_not_found", "Agent session not found")
		}
		return nil, ErrInternalWithErr(err, "session_lookup_failed", "Failed to load agent session")
	}
	return agentSessionResponse(session, s.now().UTC()), nil
}

func (s *agentService) CloseAgentSession(ctx context.Context, sessionID string) (*AgentSessionResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrBadRequest("missing_session_id", "session_id is required")
	}

	if err := s.agentRepo.CloseSession(ctx, sessionID, s.now().UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound("session_not_found", "Agent session not found")
		}
		return nil, ErrInternalWithErr(err, "session_close_failed", "Failed to close agent session")
	}
	return s.GetAgentSession(ctx, sessionID)
}

func (s *agentService) resolveSessionConnection(ctx context.Context, req AgentSessionRequest) (*domain.ConnectionWithProvider, error) {
	connectionID := strings.TrimSpace(req.ConnectionID)
	providerName := strings.TrimSpace(req.ProviderName)
	if connectionID != "" {
		id, err := uuid.Parse(connectionID)
		if err != nil {
			return nil, ErrBadRequest("invalid_connection_id", "connection_id must be a UUID")
		}
		conn, err := s.connRepo.GetWithProvider(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound("connection_not_found", "Connection not found")
			}
			return nil, ErrInternalWithErr(err, "connection_lookup_failed", "Failed to load connection")
		}
		if providerName != "" && !strings.EqualFold(providerName, conn.ProviderName) {
			return nil, ErrBadRequest("provider_mismatch", "provider_name does not match connection provider")
		}
		if conn.Status != "active" {
			return nil, ErrBadRequest("connection_not_active", "Connection not active")
		}
		return conn, nil
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" || providerName == "" {
		return nil, ErrBadRequest("missing_connection_selector", "provide connection_id or both workspace_id and provider_name")
	}

	conn, err := s.connRepo.GetActiveByWorkspaceAndProvider(ctx, workspaceID, providerName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound("connection_not_found", "Active connection not found for workspace and provider")
		}
		return nil, ErrInternalWithErr(err, "connection_lookup_failed", "Failed to load active connection")
	}
	return conn, nil
}

func sessionTTL(ttlSeconds int) (time.Duration, error) {
	if ttlSeconds < 0 {
		return 0, ErrBadRequest("invalid_ttl", "ttl_seconds must be greater than or equal to zero")
	}
	if ttlSeconds == 0 {
		return defaultAgentSessionTTL, nil
	}
	ttl := time.Duration(ttlSeconds) * time.Second
	if ttl > maxAgentSessionTTL {
		return 0, ErrBadRequest("invalid_ttl", fmt.Sprintf("ttl_seconds must not exceed %d", int(maxAgentSessionTTL/time.Second)))
	}
	return ttl, nil
}

func validAgentID(agentID string) bool {
	return validAgentIDPattern.MatchString(agentID)
}

func normalizeScopes(scopes []string) ([]string, error) {
	normalized := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			return nil, ErrBadRequest("invalid_scope", "scopes must not contain empty values")
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	return normalized, nil
}

func firstMissingScope(requested, allowed []string) string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, scope := range allowed {
		allowedSet[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, scope := range requested {
		if _, ok := allowedSet[scope]; !ok {
			return scope
		}
	}
	return ""
}

func firstConnectionMissingScope(requested, connectionScopes []string) string {
	if len(connectionScopes) == 0 {
		return ""
	}
	return firstMissingScope(requested, connectionScopes)
}

func extractAccessToken(tokenResponse map[string]interface{}) string {
	if token, ok := tokenResponse["access_token"].(string); ok {
		return token
	}
	credentials, ok := tokenResponse["credentials"].(map[string]interface{})
	if !ok {
		return ""
	}
	if token, ok := credentials["access_token"].(string); ok {
		return token
	}
	return ""
}

func newAgentSessionID() string {
	return "sess_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func agentSessionResponse(session *domain.AgentSession, now time.Time) *AgentSessionResponse {
	return &AgentSessionResponse{
		SessionID:      session.SessionID,
		AgentID:        session.AgentID,
		ConnectionID:   session.ConnectionID,
		ScopesGranted:  session.ScopesGranted,
		ExpiresAt:      session.ExpiresAt,
		Active:         session.Active(now),
		OBO:            session.OBO,
		ActingFor:      session.ActingFor,
		TenantID:       session.TenantID,
		ClearanceLevel: session.ClearanceLevel,
	}
}
