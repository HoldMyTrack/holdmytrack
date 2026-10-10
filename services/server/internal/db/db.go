// Package db owns the Postgres pool and a small embedded migration runner.
//
// Open decision recorded in services/server/README.md: golang-migrate / goose / tern vs a
// small embedded runner. Chose the embedded runner — at two migration files, a dependency
// whose entire job is "read embedded SQL files in order, skip the ones already applied" is
// not worth pulling in a library for. The one hard requirement from the README (works from
// go:embed, runs as `holdmytrack migrate`) is satisfied without one.
package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/migrations"
)

var migrationsFS = migrations.FS

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return OpenSized(ctx, databaseURL, 0)
}

// OpenSized is Open with the pool capped at maxConns connections; 0 keeps pgx's default
// (the greater of 4 and the CPU count), which is too few for `work` running several jobs at
// once, each rendering many tiles side by side.
func OpenSized(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// migrateLockKey is the advisory lock Migrate holds while it runs. Any fixed number works;
// it only has to be the same in every caller.
const migrateLockKey = 0x486d7400 // "Hmt\0"

// Migrate applies every embedded migration not yet recorded in schema_migrations, in
// filename order, each in its own transaction. It holds an advisory lock throughout, so
// two callers at once (go test runs each package's database tests in its own process) take
// turns: the second waits, then finds the first's migrations recorded and skips them.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// A session lock belongs to one connection, so the whole run uses this one.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: acquire connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("db: take migration lock: %w", err)
	}
	defer func() {
		// The connection goes back to the pool, so the lock mustn't go with it; if
		// releasing fails, close the connection, which ends the lock with its session.
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLockKey); err != nil {
			conn.Conn().Close(context.Background())
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename    TEXT PRIMARY KEY,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("db: read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 0001_..., 0002_..., ... — filename order is application order.

	for _, name := range names {
		var already bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)`, name,
		).Scan(&already); err != nil {
			return fmt.Errorf("db: check %s: %w", name, err)
		}
		if already {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("db: read %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("db: begin tx for %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("db: apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, name,
		); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("db: record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("db: commit %s: %w", name, err)
		}
	}
	return nil
}
