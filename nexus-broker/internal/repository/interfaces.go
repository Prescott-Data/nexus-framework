package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
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
}

// TokenRepository handles database operations for tokens
type TokenRepository interface {
	Upsert(ctx context.Context, token *domain.Token) error
	Get(ctx context.Context, connectionID uuid.UUID) (*domain.Token, error)
}
