package main

import (
	"database/sql"
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
}
