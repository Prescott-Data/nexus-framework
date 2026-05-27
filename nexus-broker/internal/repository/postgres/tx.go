package postgres

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type txContextKey struct{}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func withTx(ctx context.Context, tx *sqlx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

func execerFromContext(ctx context.Context, db *sqlx.DB) execer {
	if tx, ok := ctx.Value(txContextKey{}).(*sqlx.Tx); ok && tx != nil {
		return tx
	}
	return db
}
