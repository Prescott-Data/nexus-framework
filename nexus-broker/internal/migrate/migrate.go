// Package migrate applies the broker's own schema migrations on boot.
//
// The broker binary expects a particular schema. Leaving it to whoever deploys
// the broker to apply the right files in the right order means the two can
// disagree, and the disagreement is only discovered when a request reaches a
// column that is not there. Applying them here keeps the schema a property of
// the binary rather than of the deployment.
//
// Which migrations have run is recorded in schema_migrations, keyed by
// filename, so running twice is a no-op and a partly-migrated database
// continues from where it stopped.
package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// DefaultDir is where the image places the migrations.
const DefaultDir = "/app/migrations"

const createLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    filename    TEXT PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

var leadingNumber = regexp.MustCompile(`^(\d+)`)

// Result describes what a run did, for logging by the caller.
type Result struct {
	Applied []string
	Skipped int
}

// Run applies every migration in dir that the database has not recorded yet.
//
// Ordering is by leading number, then filename, so 9 precedes 10 and two files
// sharing a number still have a stable order between them. Each migration and
// its ledger entry share a transaction: a migration that fails leaves neither a
// partial schema nor a claim that it ran.
func Run(db *sql.DB, dir string) (Result, error) {
	var result Result

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, fmt.Errorf("migrations directory %s is missing: the image "+
				"should carry the schema its binary expects", dir)
		}
		return result, fmt.Errorf("read migrations directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return result, fmt.Errorf("no .sql migrations found in %s", dir)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := order(names[i]), order(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})

	if _, err := db.Exec(createLedger); err != nil {
		return result, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := alreadyApplied(db)
	if err != nil {
		return result, err
	}

	for _, name := range names {
		if _, done := applied[name]; done {
			result.Skipped++
			continue
		}
		statements, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return result, fmt.Errorf("read %s: %w", name, err)
		}
		if err := apply(db, name, string(statements)); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, name)
	}
	return result, nil
}

func apply(db *sql.DB, name, statements string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(statements); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

func alreadyApplied(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[name] = struct{}{}
	}
	return applied, rows.Err()
}

func order(name string) int {
	if match := leadingNumber.FindString(name); match != "" {
		if n, err := strconv.Atoi(match); err == nil {
			return n
		}
	}
	return 0
}
