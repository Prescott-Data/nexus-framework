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

// Upsert stores the credential blob for a connection.
//
// The write is conditional on the connection not being revoked, and takes a
// FOR SHARE lock on the connection row to make that check binding. Revocation
// runs MarkRevoked (row-exclusive on the same row) as the first statement of
// its transaction, so an in-flight refresh or OAuth exchange either commits
// before revocation begins — and has its token deleted by the revoke — or
// blocks until revocation commits and is then rejected here. Without this a
// racing refresh could recreate the credential that revocation just destroyed.
func (r *tokenRepository) Upsert(ctx context.Context, token *domain.Token) error {
	result, err := execerFromContext(ctx, r.db).ExecContext(ctx, `
		WITH live_connection AS (
			SELECT id FROM connections WHERE id = $1 AND revoked_at IS NULL FOR SHARE
		)
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		SELECT $1, $2, $3 FROM live_connection
		ON CONFLICT (connection_id)
		DO UPDATE SET
			encrypted_data = EXCLUDED.encrypted_data,
			expires_at     = EXCLUDED.expires_at,
			created_at     = NOW()`,
		token.ConnectionID, token.EncryptedData, token.ExpiresAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrConnectionRevoked
	}
	return nil
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

// Delete removes the credential row for a connection. Missing rows are not an
// error so that revocation can be retried safely.
func (r *tokenRepository) Delete(ctx context.Context, connectionID uuid.UUID) error {
	_, err := execerFromContext(ctx, r.db).ExecContext(ctx, "DELETE FROM tokens WHERE connection_id = $1", connectionID)
	return err
}
