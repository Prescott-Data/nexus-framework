package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
)

type connectionRepository struct {
	db *sqlx.DB
}

// NewConnectionRepository creates a new Postgres ConnectionRepository
func NewConnectionRepository(db *sqlx.DB) repository.ConnectionRepository {
	return &connectionRepository{db: db}
}

func (r *connectionRepository) Create(ctx context.Context, conn *domain.Connection) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connections (id, workspace_id, provider_id, code_verifier, scopes, return_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		conn.ID, conn.WorkspaceID, conn.ProviderID, conn.CodeVerifier, pq.Array(conn.Scopes), conn.ReturnURL, conn.ExpiresAt)
	return err
}

func (r *connectionRepository) GetPending(ctx context.Context, id uuid.UUID) (*domain.Connection, error) {
	var conn domain.Connection
	err := r.db.QueryRowContext(ctx, `
		SELECT id, code_verifier, return_url, provider_id, scopes
		FROM connections
		WHERE id = $1 AND status = 'pending' AND expires_at > NOW()`, id).
		Scan(&conn.ID, &conn.CodeVerifier, &conn.ReturnURL, &conn.ProviderID, pq.Array(&conn.Scopes))
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

func (r *connectionRepository) GetWithProvider(ctx context.Context, id uuid.UUID) (*domain.ConnectionWithProvider, error) {
	var conn domain.ConnectionWithProvider
	err := r.db.QueryRowContext(ctx, `
		SELECT c.id, c.provider_id, c.status, c.scopes, c.return_url,
		       p.auth_type, COALESCE(p.auth_header, ''), COALESCE(p.api_base_url, ''), COALESCE(p.user_info_endpoint, ''), p.params
		FROM connections c
		JOIN provider_profiles p ON p.id = c.provider_id
		WHERE c.id = $1`, id).
		Scan(&conn.ID, &conn.ProviderID, &conn.Status, pq.Array(&conn.Scopes), &conn.ReturnURL,
			&conn.AuthType, &conn.AuthHeader, &conn.APIBaseURL, &conn.UserInfoEndpoint, &conn.ProviderParams)
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

func (r *connectionRepository) GetReturnURL(ctx context.Context, id uuid.UUID) (string, error) {
	var returnURL string
	err := r.db.QueryRowContext(ctx, "SELECT return_url FROM connections WHERE id = $1", id).Scan(&returnURL)
	return returnURL, err
}

func (r *connectionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE connections SET status = $1, updated_at = NOW() WHERE id = $2", status, id)
	return err
}
