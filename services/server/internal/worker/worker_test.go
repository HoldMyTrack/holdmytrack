package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
)

// These read TEST_DATABASE_URL and skip without it, like internal/httpapi's database tests
// (docs/DEVELOPMENT.md). Their jobs run_after a date long past, so the claim's ORDER BY reaches
// them before any job another package's tests leave queued in the same database.

func testPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	var userID string
	email := fmt.Sprintf("worker-test-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete account: %v", err)
		}
	})
	return pool, userID
}

type jobRow struct {
	state, lastError string
	attempts         int
}

func insertJob(t *testing.T, pool *pgxpool.Pool, userID, runAfter string, attempts int, lockedAt *time.Time) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO jobs (kind, user_id, payload, run_after, attempts, locked_at)
		VALUES ('test_unknown', $1, '{}', $2, $3, $4) RETURNING id
	`, userID, runAfter, attempts, lockedAt).Scan(&id); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return id
}

func readJob(t *testing.T, pool *pgxpool.Pool, id int64) jobRow {
	t.Helper()
	var r jobRow
	var lastError *string
	if err := pool.QueryRow(context.Background(),
		`SELECT state, last_error, attempts FROM jobs WHERE id = $1`, id).Scan(&r.state, &lastError, &r.attempts); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if lastError != nil {
		r.lastError = *lastError
	}
	return r
}

func TestClaimSkipsAJobStillRunningAndFailsOneThatKeptCrashing(t *testing.T) {
	pool, userID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	fresh := time.Now()
	stale := time.Now().Add(-2 * claimLease)
	running := insertJob(t, pool, userID, "2000-01-01T00:00:00Z", 1, &fresh)
	crashed := insertJob(t, pool, userID, "2000-01-02T00:00:00Z", maxAttempts, &stale)
	next := insertJob(t, pool, userID, "2000-01-03T00:00:00Z", 0, nil)

	// First claim: the running job is passed over, and the one that took its worker down
	// maxAttempts times is failed without being run again.
	if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || !processed {
		t.Fatalf("claim 1: processed=%v err=%v", processed, err)
	}
	if r := readJob(t, pool, running); r.state != "pending" || r.attempts != 1 {
		t.Errorf("running job = %+v, want untouched", r)
	}
	if r := readJob(t, pool, crashed); r.state != "failed" || !strings.Contains(r.lastError, "stopped while running") {
		t.Errorf("crashed job = %+v, want failed as abandoned", r)
	}

	// Second claim: the next job runs, fails (unknown kind), and is counted once.
	if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || !processed {
		t.Fatalf("claim 2: processed=%v err=%v", processed, err)
	}
	if r := readJob(t, pool, next); r.state != "failed" || r.attempts != 1 || !strings.Contains(r.lastError, "unhandled job kind") {
		t.Errorf("next job = %+v, want failed after one attempt", r)
	}
	if r := readJob(t, pool, running); r.state != "pending" {
		t.Errorf("running job = %+v, want still pending", r)
	}
}
