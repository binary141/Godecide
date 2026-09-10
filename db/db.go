// Package db manages the postgres connection and applies SQL migrations
// embedded from db/migrations at startup.
package db

import (
	"embed"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrations embed.FS

var DB *sqlx.DB

func Connect() error {
	host := getEnv("POSTGRES_HOST", "db")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "dev")
	password := getEnv("POSTGRES_PASSWORD", "secret")
	dbname := getEnv("POSTGRES_DB", "app")

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname,
	)

	var err error
	DB, err = sqlx.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("db: open: %w", err)
	}

	if err = DB.Ping(); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}

	log.Println("connected to database")
	return nil
}

func RunMigrations() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		var count int
		if err := DB.Get(&count, "SELECT COUNT(*) FROM schema_migrations WHERE version = $1", file); err != nil {
			return fmt.Errorf("check migration %s: %w", file, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrations.ReadFile("migrations/" + file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		tx, err := DB.Beginx()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", file, err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			if rollErr := tx.Rollback(); rollErr != nil {
				return fmt.Errorf("exec migration %s: %w. rollback migration: %w", file, err, rollErr)
			}
			return fmt.Errorf("exec migration %s: %w", file, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", file); err != nil {
			if rollErr := tx.Rollback(); rollErr != nil {
				return fmt.Errorf("record migration %s: %w. rollback migration: %w", file, err, rollErr)
			}
			return fmt.Errorf("record migration %s: %w", file, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", file, err)
		}

		log.Printf("applied migration: %s", file)
	}

	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
