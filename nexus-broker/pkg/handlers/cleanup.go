package handlers

import (
	"context"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
)

// StartOrphanTokenCleanup periodically removes token rows whose parent
// connection has been deleted. This is a safety net for rows that the UPSERT
// logic cannot reach (e.g. a connection was deleted but its token row lingers).
// The query uses a LEFT JOIN rather than age-based deletion to avoid removing
// valid tokens for slow-refreshing connections.
func StartOrphanTokenCleanup(ctx context.Context, db *sqlx.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			result, err := db.ExecContext(ctx, `
				DELETE FROM tokens t
				WHERE NOT EXISTS (
					SELECT 1 FROM connections c WHERE c.id = t.connection_id
				)`)
			if err != nil {
				log.Printf("orphan token cleanup failed: %v", err)
				continue
			}
			if rows, _ := result.RowsAffected(); rows > 0 {
				log.Printf("orphan token cleanup: deleted %d orphaned rows", rows)
			}
		case <-ctx.Done():
			return
		}
	}
}
