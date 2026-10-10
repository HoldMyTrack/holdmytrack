package db

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Several Migrate calls at once on an empty database all succeed, each migration applied
// once: the advisory lock makes them take turns. Reads TEST_DATABASE_URL and skips without
// it, like the other database tests (docs/DEVELOPMENT.md), and works in a database of its
// own, created and dropped here, so every migration is new to it.
func TestMigrateConcurrent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(admin.Close)

	name := fmt.Sprintf("holdmytrack_migrate_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Errorf("drop database: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.Database = name

	const callers = 4
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		t.Cleanup(pool.Close)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = Migrate(ctx, pool)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
}
