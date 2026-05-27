package postgres

import (
	"context"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type tokenRepository struct {
	db *sqlx.DB
}

// NewTokenRepository creates a new Postgres TokenRepository
func NewTokenRepository(db *sqlx.DB) repository.TokenRepository {
	return &tokenRepository{db: db}
}

func (r *tokenRepository) Upsert(ctx context.Context, token *domain.Token) error {
	_, err := execerFromContext(ctx, r.db).ExecContext(ctx, `
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (connection_id)
		DO UPDATE SET
			encrypted_data = EXCLUDED.encrypted_data,
			expires_at     = EXCLUDED.expires_at,
			created_at     = NOW()`,
		token.ConnectionID, token.EncryptedData, token.ExpiresAt)
	return err
}

func (r *tokenRepository) Get(ctx context.Context, connectionID uuid.UUID) (*domain.Token, error) {
	var token domain.Token
	err := r.db.QueryRowContext(ctx, "SELECT encrypted_data, expires_at FROM tokens WHERE connection_id = $1", connectionID).
		Scan(&token.EncryptedData, &token.ExpiresAt)
	if err != nil {
		return nil, err
	}
	token.ConnectionID = connectionID
	return &token, nil
}
