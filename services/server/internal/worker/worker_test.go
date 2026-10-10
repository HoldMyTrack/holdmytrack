package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
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
	return insertJobOfKind(t, pool, userID, "test_unknown", `{}`, runAfter, attempts, lockedAt)
}

func insertJobOfKind(t *testing.T, pool *pgxpool.Pool, userID, kind, payload, runAfter string, attempts int, lockedAt *time.Time) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO jobs (kind, user_id, payload, run_after, attempts, locked_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, kind, userID, payload, runAfter, attempts, lockedAt).Scan(&id); err != nil {
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
	// The running job is another account's: an account with a job running has its others
	// held back anyway (TestClaimRunsOneJobPerAccountAtATime).
	_, otherID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	fresh := time.Now()
	stale := time.Now().Add(-2 * claimLease)
	running := insertJob(t, pool, otherID, "2000-01-01T00:00:00Z", 1, &fresh)
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

// While one of an account's jobs runs, its next one waits even if it's first in line, and
// another account's job is claimed instead.
func TestClaimRunsOneJobPerAccountAtATime(t *testing.T) {
	pool, busyID := testPool(t)
	_, otherID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	fresh := time.Now()
	insertJob(t, pool, busyID, "2000-01-01T00:00:00Z", 1, &fresh)
	held := insertJob(t, pool, busyID, "2000-01-02T00:00:00Z", 0, nil)
	other := insertJob(t, pool, otherID, "2000-01-03T00:00:00Z", 0, nil)

	if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || !processed {
		t.Fatalf("claim 1: processed=%v err=%v", processed, err)
	}
	if r := readJob(t, pool, held); r.state != "pending" || r.attempts != 0 {
		t.Errorf("busy account's next job = %+v, want held back", r)
	}
	if r := readJob(t, pool, other); r.attempts != 1 {
		t.Errorf("other account's job = %+v, want claimed", r)
	}
	// With the busy account's job still running, nothing else is claimable.
	if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || processed {
		t.Fatalf("claim 2: processed=%v err=%v, want nothing claimable", processed, err)
	}
}

// Accounts take turns: one with a long queue doesn't keep a newer account waiting behind all
// of it, and two accounts with queues alternate.
func TestClaimTakesAccountsInTurn(t *testing.T) {
	pool, bigID := testPool(t)
	_, smallID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	var big []int64
	for i := range 4 {
		big = append(big, insertJob(t, pool, bigID, fmt.Sprintf("2000-01-0%dT00:00:00Z", i+1), 0, nil))
	}
	// Queued after all of the big account's, as a new user's upload arriving mid-import.
	small := []int64{
		insertJob(t, pool, smallID, "2000-02-01T00:00:00Z", 0, nil),
		insertJob(t, pool, smallID, "2000-02-02T00:00:00Z", 0, nil),
	}

	order := func() []int64 {
		var ids []int64
		rows, err := pool.Query(ctx, `
			SELECT id FROM jobs WHERE user_id IN ($1, $2) AND state <> 'pending'
			ORDER BY locked_at, id`, bigID, smallID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	for i := range 6 {
		if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || !processed {
			t.Fatalf("claim %d: processed=%v err=%v", i+1, processed, err)
		}
	}
	// Both never served: the earlier run_after goes first. Then they alternate, the
	// least recently served first, until the small account has nothing left.
	want := []int64{big[0], small[0], big[1], small[1], big[2], big[3]}
	if got := order(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("claim order = %v, want %v", got, want)
	}
}

// Several loops claiming at once (Run's concurrency) never take the same job twice.
func TestConcurrentClaimsTakeEachJobOnce(t *testing.T) {
	pool, firstID := testPool(t)
	accounts := []string{firstID}
	for range 2 {
		_, id := testPool(t)
		accounts = append(accounts, id)
	}
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	var ids []int64
	for i := range 10 {
		for _, acct := range accounts {
			ids = append(ids, insertJob(t, pool, acct, fmt.Sprintf("2000-01-%02dT00:00:00Z", i+1), 0, nil))
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idle := 0; idle < 20; {
				processed, err := claimAndRunOne(ctx, pool, nil, log)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if processed {
					idle = 0
				} else {
					idle++ // another loop holds the only claimable account's job for now
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if r := readJob(t, pool, id); r.state == "pending" || r.attempts != 1 {
			t.Errorf("job %d = %+v, want claimed exactly once", id, r)
		}
	}
}

// A render runs in a lane of its own: one claimed doesn't hold back the main lane, for the same
// account or another, and the main lane never claims a render.
func TestRenderLaneRunsBesideTheMainLane(t *testing.T) {
	pool, renderingID := testPool(t)
	_, otherID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	fresh := time.Now()
	insertJobOfKind(t, pool, renderingID, "render_fog", `{}`, "2000-01-01T00:00:00Z", 1, &fresh)
	queued := insertJobOfKind(t, pool, otherID, "render_fog", `{}`, "2000-01-02T00:00:00Z", 0, nil)
	own := insertJob(t, pool, renderingID, "2000-01-03T00:00:00Z", 0, nil)
	other := insertJob(t, pool, otherID, "2000-01-04T00:00:00Z", 0, nil)

	for i := range 2 {
		if processed, err := claimAndRun(ctx, pool, nil, log, laneMain); err != nil || !processed {
			t.Fatalf("main claim %d: processed=%v err=%v", i+1, processed, err)
		}
	}
	if r := readJob(t, pool, own); r.attempts != 1 {
		t.Errorf("rendering account's own job = %+v, want claimed beside its render", r)
	}
	if r := readJob(t, pool, other); r.attempts != 1 {
		t.Errorf("other account's job = %+v, want claimed", r)
	}
	if r := readJob(t, pool, queued); r.state != "pending" || r.attempts != 0 {
		t.Errorf("queued render = %+v, want left to the render lane", r)
	}
}

// Render loops are fair across accounts like the main lane: while one account's render runs,
// a second loop takes another account's, not the first account's next.
func TestRenderLoopsTakeAccountsInTurn(t *testing.T) {
	pool, firstID := testPool(t)
	_, secondID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	fresh := time.Now()
	insertJobOfKind(t, pool, firstID, "render_fog", `{}`, "2000-01-01T00:00:00Z", 1, &fresh)
	held := insertJobOfKind(t, pool, firstID, "render_fog", fmt.Sprintf(`{"user_id":%q}`, firstID), "2000-01-02T00:00:00Z", 0, nil)
	next := insertJobOfKind(t, pool, secondID, "render_fog", fmt.Sprintf(`{"user_id":%q}`, secondID), "2000-01-03T00:00:00Z", 0, nil)

	// Neither account has a dirty tile, so the render is done without touching the store.
	if processed, err := claimAndRun(ctx, pool, nil, log, laneRender); err != nil || !processed {
		t.Fatalf("render claim: processed=%v err=%v", processed, err)
	}
	if r := readJob(t, pool, next); r.state != "done" || r.attempts != 1 {
		t.Errorf("second account's render = %+v, want claimed and done", r)
	}
	if r := readJob(t, pool, held); r.state != "pending" || r.attempts != 0 {
		t.Errorf("first account's next render = %+v, want held back", r)
	}
}

// A deleted account's jobs are never claimed, even before the purge (account_purge.go) has
// dropped them: the claim passes over them to the next account's job.
func TestClaimSkipsADeletedAccountsJobs(t *testing.T) {
	pool, deletedID := testPool(t)
	_, liveID := testPool(t)
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, deletedID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	skipped := insertJob(t, pool, deletedID, "2000-01-01T00:00:00Z", 0, nil)
	next := insertJob(t, pool, liveID, "2000-01-02T00:00:00Z", 0, nil)

	if processed, err := claimAndRunOne(ctx, pool, nil, log); err != nil || !processed {
		t.Fatalf("claim: processed=%v err=%v", processed, err)
	}
	if r := readJob(t, pool, skipped); r.state != "pending" || r.attempts != 0 {
		t.Errorf("deleted account's job = %+v, want never claimed", r)
	}
	if r := readJob(t, pool, next); r.state != "failed" || r.attempts != 1 {
		t.Errorf("live account's job = %+v, want claimed (and failed as an unknown kind)", r)
	}
}

// A job that panics fails on its own instead of taking the worker down with it. An ingest
// job over a nil object store panics on its first fetch — the same kind of nil dereference or
// index out of range a malformed file used to cause inside a parser.
func TestPanickingJobFailsAlone(t *testing.T) {
	pool, userID := testPool(t)
	payload := fmt.Sprintf(`{"user_id":%q,"source":"upload","source_detail":"x.gpx","external_id":"panic-test","raw_payload_key":"raw/none.gpx"}`, userID)
	id := insertJobOfKind(t, pool, userID, "ingest", payload, "2000-01-01T00:00:00Z", 0, nil)

	if processed, err := claimAndRunOne(context.Background(), pool, nil, slog.New(slog.DiscardHandler)); err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	r := readJob(t, pool, id)
	if r.state != "failed" || !strings.HasPrefix(r.lastError, "panic:") || r.attempts != 1 {
		t.Fatalf("job = %+v, want failed with the panic as its error", r)
	}
}

// A job cut off by shutdown is handed back for the next start: still pending, unlocked, its
// attempt not counted against it.
func TestShutdownReleasesTheRunningJob(t *testing.T) {
	pool, userID := testPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	running := make(chan struct{})
	jobRunner = func(ctx context.Context, _ *pgxpool.Pool, _ *storage.Store, _ *slog.Logger, _ job) error {
		close(running)
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { jobRunner = runJob })
	id := insertJob(t, pool, userID, "2000-01-01T00:00:00Z", 0, nil)

	go func() {
		<-running
		cancel()
	}()
	if processed, err := claimAndRunOne(ctx, pool, nil, slog.New(slog.DiscardHandler)); err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	var lockedAt *time.Time
	r := readJob(t, pool, id)
	if err := pool.QueryRow(context.Background(), `SELECT locked_at FROM jobs WHERE id = $1`, id).Scan(&lockedAt); err != nil {
		t.Fatal(err)
	}
	if r.state != "pending" || r.attempts != 0 || lockedAt != nil {
		t.Fatalf("job = %+v locked_at %v, want pending, unlocked, no attempt counted", r, lockedAt)
	}
}

func TestRenderFogCoalescesWhileUnclaimed(t *testing.T) {
	pool, userID := testPool(t)
	ctx := context.Background()
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM jobs WHERE kind = 'render_fog' AND user_id = $1 AND state = 'pending'`, userID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	for range 3 {
		if err := ingest.EnqueueRenderFog(ctx, pool, userID); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	if n := count(); n != 1 {
		t.Fatalf("three enqueues with none claimed left %d jobs, want 1", n)
	}

	// Claimed, it may already have read the dirty flags: the next change needs a job of its own.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_at = NOW() WHERE kind = 'render_fog' AND user_id = $1`, userID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := ingest.EnqueueRenderFog(ctx, pool, userID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if n := count(); n != 2 {
		t.Fatalf("an enqueue behind a claimed job left %d jobs, want 2", n)
	}
}
