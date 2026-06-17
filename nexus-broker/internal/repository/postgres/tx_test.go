package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
)

func TestInTx_Commit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	connRepo := NewConnectionRepository(sqlxDB)
	tokenRepo := NewTokenRepository(sqlxDB)

	connID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE connections SET status = $1, updated_at = NOW() WHERE id = $2")).
		WithArgs("active", connID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (connection_id)
		DO UPDATE SET
			encrypted_data = EXCLUDED.encrypted_data,
			expires_at     = EXCLUDED.expires_at,
			created_at     = NOW()`)).
		WithArgs(connID, "enc", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	runner, ok := connRepo.(interface {
		InTx(ctx context.Context, fn func(context.Context) error) error
	})
	if !ok {
		t.Fatal("connection repo does not implement InTx")
	}

	err = runner.InTx(context.Background(), func(ctx context.Context) error {
		if err := connRepo.UpdateStatus(ctx, connID, "active"); err != nil {
			return err
		}
		expires := time.Now().Add(time.Hour)
		return tokenRepo.Upsert(ctx, &domain.Token{
			ConnectionID:  connID,
			EncryptedData: "enc",
			ExpiresAt:     &expires,
		})
	})
	if err != nil {
		t.Fatalf("InTx commit flow failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestInTx_Rollback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	connRepo := NewConnectionRepository(sqlxDB)
	connID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE connections SET status = $1, updated_at = NOW() WHERE id = $2")).
		WithArgs("active", connID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	runner, ok := connRepo.(interface {
		InTx(ctx context.Context, fn func(context.Context) error) error
	})
	if !ok {
		t.Fatal("connection repo does not implement InTx")
	}

	err = runner.InTx(context.Background(), func(ctx context.Context) error {
		if err := connRepo.UpdateStatus(ctx, connID, "active"); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("expected rollback error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
