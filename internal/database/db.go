// Package database holds the Postgres side of godrop: the connection, the
// schema, and the job history.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Open connects to Postgres and checks the connection with a ping.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	// sql.DB is a pool of connections, not one connection.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate runs every embedded migrations/*.sql file in name order. The files
// must be idempotent (CREATE ... IF NOT EXISTS), because they run on every start.
func Migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations") // sorted by file name
	if err != nil {
		return err
	}

	for _, e := range entries {
		query, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(query)); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	return nil
}
