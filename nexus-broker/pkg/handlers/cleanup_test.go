package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestStartOrphanTokenCleanup_DeletesOrphans(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")

	mock.ExpectExec("DELETE FROM tokens").
		WillReturnResult(sqlmock.NewResult(0, 3))

	ctx, cancel := context.WithCancel(context.Background())

	// Use a longer interval to ensure it only ticks once during our sleep
	go StartOrphanTokenCleanup(ctx, sqlxDB, 200*time.Millisecond)

	// Wait enough for the first tick to fire, but less than two ticks
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Wait a bit for the goroutine to finish after cancel
	time.Sleep(50 * time.Millisecond)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStartOrphanTokenCleanup_StopsOnContextCancel(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		StartOrphanTokenCleanup(ctx, sqlxDB, 1*time.Hour)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// goroutine exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup goroutine did not exit after context cancellation")
	}
}
