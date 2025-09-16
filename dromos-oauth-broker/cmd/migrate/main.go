package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	file := flag.String("file", "", "Path to SQL migration file to apply")
	dsn := flag.String("dsn", "", "Postgres DSN (overrides DATABASE_URL)")
	flag.Parse()

	if *file == "" {
		log.Fatal("-file is required")
	}

	url := *dsn
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		log.Fatal("DATABASE_URL or -dsn is required")
	}

	sqlBytes, err := ioutil.ReadFile(*file)
	if err != nil {
		log.Fatalf("read migration: %v", err)
	}

	db, err := sql.Open("postgres", url)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping: %v", err)
	}

	if _, err := db.Exec(string(sqlBytes)); err != nil {
		// Many ALTER IF NOT EXISTS are safe to re-run; surface error for visibility
		log.Fatalf("exec migration: %v", err)
	}
	fmt.Println("migration applied:", *file)

	// Verify columns exist
	verifyQuery := `SELECT column_name FROM information_schema.columns WHERE table_name='provider_profiles' AND column_name IN ('issuer','enable_discovery') ORDER BY column_name`
	rows, err := db.Query(verifyQuery)
	if err != nil {
		log.Fatalf("verify query: %v", err)
	}
	defer rows.Close()
	found := map[string]bool{"issuer": false, "enable_discovery": false}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			log.Fatalf("scan: %v", err)
		}
		found[col] = true
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("rows: %v", err)
	}
	if !(found["issuer"] && found["enable_discovery"]) {
		log.Fatal(errors.New("verification failed: expected columns not found"))
	}
	fmt.Println("verification ok: issuer and enable_discovery present")
}
