package repository

import (
	"context"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/google/uuid"
)

// ConnectionRepository handles database operations for connections
type ConnectionRepository interface {
	Create(ctx context.Context, conn *domain.Connection) error
	GetPending(ctx context.Context, id uuid.UUID) (*domain.Connection, error)
	GetWithProvider(ctx context.Context, id uuid.UUID) (*domain.ConnectionWithProvider, error)
	GetReturnURL(ctx context.Context, id uuid.UUID) (string, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	CountByStatus(ctx context.Context) (map[string]int64, error)
	GetActiveByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (*domain.ConnectionWithProvider, error)
	GetForHealthCheck(ctx context.Context, limit int) ([]*domain.ConnectionWithProvider, error)
	UpdateHealthStatus(ctx context.Context, id uuid.UUID, status string) error
	ListByWorkspace(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error)
	// DeactivateOtherActive marks all active connections for the same workspace+provider
	// as "superseded", excluding the connection that just became active.
	DeactivateOtherActive(ctx context.Context, workspaceID string, providerID uuid.UUID, exceptID uuid.UUID) error
}

// TokenRepository handles database operations for tokens
type TokenRepository interface {
	Upsert(ctx context.Context, token *domain.Token) error
	Get(ctx context.Context, connectionID uuid.UUID) (*domain.Token, error)
}

// AgentRepository handles database operations for agent principals and sessions.
type AgentRepository interface {
	CreateAgent(ctx context.Context, agent *domain.Agent) error
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	CreateSession(ctx context.Context, session *domain.AgentSession) error
	GetSession(ctx context.Context, sessionID string) (*domain.AgentSession, error)
	CloseSession(ctx context.Context, sessionID string, closedAt time.Time) error
}
