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
		       p.name, p.auth_type, COALESCE(p.auth_header, ''), COALESCE(p.api_base_url, ''), COALESCE(p.user_info_endpoint, ''), p.params,
		       COALESCE(c.health_status, 'unknown')
		FROM connections c
		JOIN provider_profiles p ON p.id = c.provider_id
		WHERE c.id = $1`, id).
		Scan(&conn.ID, &conn.ProviderID, &conn.Status, pq.Array(&conn.Scopes), &conn.ReturnURL,
			&conn.ProviderName, &conn.AuthType, &conn.AuthHeader, &conn.APIBaseURL, &conn.UserInfoEndpoint, &conn.ProviderParams,
			&conn.HealthStatus)
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

func (r *connectionRepository) GetActiveByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (*domain.ConnectionWithProvider, error) {
	var conn domain.ConnectionWithProvider
	err := r.db.QueryRowContext(ctx, `
		SELECT c.id, c.provider_id, c.status, c.scopes, c.return_url,
		       p.name, p.auth_type, COALESCE(p.auth_header, ''), COALESCE(p.api_base_url, ''), COALESCE(p.user_info_endpoint, ''), p.params,
		       COALESCE(c.health_status, 'unknown')
		FROM connections c
		JOIN provider_profiles p ON p.id = c.provider_id
		WHERE c.workspace_id = $1 AND p.name = $2 AND c.status = 'active'
		ORDER BY c.updated_at DESC
		LIMIT 1`, workspaceID, providerName).
		Scan(&conn.ID, &conn.ProviderID, &conn.Status, pq.Array(&conn.Scopes), &conn.ReturnURL,
			&conn.ProviderName, &conn.AuthType, &conn.AuthHeader, &conn.APIBaseURL, &conn.UserInfoEndpoint, &conn.ProviderParams,
			&conn.HealthStatus)
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

func (r *connectionRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM connections GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func (r *connectionRepository) GetForHealthCheck(ctx context.Context, limit int) ([]*domain.ConnectionWithProvider, error) {
	var rows []domain.ConnectionWithProvider
	// Fetch active connections that haven't been checked in the last hour,
	// or have never been checked, prioritizing the oldest checks first.
	query := `
		SELECT c.id, c.workspace_id, c.provider_id, c.scopes, c.return_url, c.status, c.expires_at,
		       c.last_health_check_at, COALESCE(c.health_status, 'unknown'),
		       p.name, p.auth_type, COALESCE(p.auth_header, ''), COALESCE(p.api_base_url, ''), COALESCE(p.user_info_endpoint, ''), p.params
		FROM connections c
		JOIN provider_profiles p ON c.provider_id = p.id
		WHERE c.status = 'active'
		  AND (c.last_health_check_at IS NULL OR c.last_health_check_at < NOW() - INTERVAL '1 hour')
		ORDER BY c.last_health_check_at ASC NULLS FIRST
		LIMIT $1
	`
	dbRows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()

	for dbRows.Next() {
		var conn domain.ConnectionWithProvider
		err := dbRows.Scan(
			&conn.ID, &conn.WorkspaceID, &conn.ProviderID, pq.Array(&conn.Scopes), &conn.ReturnURL, &conn.Status, &conn.ExpiresAt,
			&conn.LastHealthCheckAt, &conn.HealthStatus,
			&conn.ProviderName, &conn.AuthType, &conn.AuthHeader, &conn.APIBaseURL, &conn.UserInfoEndpoint, &conn.ProviderParams,
		)
		if err != nil {
			return nil, err
		}
		rows = append(rows, conn)
	}

	// Returning pointers as per interface
	var ptrRows []*domain.ConnectionWithProvider
	for i := range rows {
		ptrRows = append(ptrRows, &rows[i])
	}

	return ptrRows, nil
}

func (r *connectionRepository) UpdateHealthStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE connections 
		SET health_status = $1, last_health_check_at = NOW(), updated_at = NOW()
		WHERE id = $2`, status, id)
	return err
}

func (r *connectionRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error) {
	query := `
		SELECT c.id, c.provider_id, p.name, p.auth_type, c.status, c.scopes,
		       COALESCE(c.health_status, 'unknown'), c.last_health_check_at,
		       c.created_at, c.updated_at
		FROM connections c
		JOIN provider_profiles p ON c.provider_id = p.id AND p.deleted_at IS NULL
		WHERE c.workspace_id = $1 AND c.status != 'pending'
		ORDER BY c.updated_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []domain.ConnectionSummary
	for rows.Next() {
		var s domain.ConnectionSummary
		err := rows.Scan(
			&s.ID, &s.ProviderID, &s.ProviderName, &s.AuthType, &s.Status, pq.Array(&s.Scopes),
			&s.HealthStatus, &s.LastHealthCheckAt,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil for clean JSON
	if summaries == nil {
		summaries = []domain.ConnectionSummary{}
	}

	return summaries, nil
}
