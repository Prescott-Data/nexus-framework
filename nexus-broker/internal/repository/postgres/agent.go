package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type agentRepository struct {
	db *sqlx.DB
}

// NewAgentRepository creates a new Postgres AgentRepository.
func NewAgentRepository(db *sqlx.DB) repository.AgentRepository {
	return &agentRepository{db: db}
}

func (r *agentRepository) CreateAgent(ctx context.Context, agent *domain.Agent) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO agents (id, description, allowed_scopes, active)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at`,
		agent.ID, agent.Description, pq.Array(agent.AllowedScopes), agent.Active).
		Scan(&agent.CreatedAt)
}

func (r *agentRepository) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	var agent domain.Agent
	err := r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(description, ''), allowed_scopes, created_at, active
		FROM agents
		WHERE id = $1`, id).
		Scan(&agent.ID, &agent.Description, pq.Array(&agent.AllowedScopes), &agent.CreatedAt, &agent.Active)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (r *agentRepository) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, COALESCE(description, ''), allowed_scopes, created_at, active
		FROM agents
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	agents := []domain.Agent{}
	for rows.Next() {
		var agent domain.Agent
		if err := rows.Scan(&agent.ID, &agent.Description, pq.Array(&agent.AllowedScopes), &agent.CreatedAt, &agent.Active); err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (r *agentRepository) CreateSession(ctx context.Context, session *domain.AgentSession) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO agent_sessions (
			session_id, agent_id, connection_id, scopes_granted, expires_at,
			closed_at, obo, acting_for, tenant_id, clearance_level
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at`,
		session.SessionID,
		session.AgentID,
		session.ConnectionID,
		pq.Array(session.ScopesGranted),
		session.ExpiresAt,
		session.ClosedAt,
		session.OBO,
		nullString(session.ActingFor),
		nullString(session.TenantID),
		session.ClearanceLevel,
	).Scan(&session.CreatedAt)
}

func (r *agentRepository) GetSession(ctx context.Context, sessionID string) (*domain.AgentSession, error) {
	var session domain.AgentSession
	err := r.db.QueryRowContext(ctx, `
		SELECT session_id, agent_id, connection_id, scopes_granted, expires_at,
		       closed_at, obo, COALESCE(acting_for, ''), COALESCE(tenant_id, ''),
		       clearance_level, created_at
		FROM agent_sessions
		WHERE session_id = $1`, sessionID).
		Scan(
			&session.SessionID,
			&session.AgentID,
			&session.ConnectionID,
			pq.Array(&session.ScopesGranted),
			&session.ExpiresAt,
			&session.ClosedAt,
			&session.OBO,
			&session.ActingFor,
			&session.TenantID,
			&session.ClearanceLevel,
			&session.CreatedAt,
		)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *agentRepository) CloseSession(ctx context.Context, sessionID string, closedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_sessions
		SET closed_at = COALESCE(closed_at, $2)
		WHERE session_id = $1`,
		sessionID, closedAt)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
