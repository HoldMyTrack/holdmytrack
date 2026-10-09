// Package worker is cmd/holdmytrack work: dequeues jobs with FOR UPDATE SKIP LOCKED
// (IMPLEMENTATION.md §3.8, §4.1) and runs internal/ingest for `ingest` and `edit_track` jobs.
// No broker, per §1.1 — this poll loop against idx_jobs_runnable is the whole queue.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storycopy"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/unpack"
)

const pollInterval = 500 * time.Millisecond

// claimLease is how long a claimed job stays out of other claims. A job keeps state =
// 'pending' while it runs (the handlers that show progress count on that), so locked_at is
// what marks it taken: a fresh one excludes the row, a stale one means the worker that took
// it died mid-run (killed, out of memory) and the job is up for grabs again. Far longer than
// any job should run, so a slow one isn't handed to a second worker while the first is busy.
const claimLease = 30 * time.Minute

// maxAttempts bounds how many times a job is claimed without finishing. Attempts count at
// claim time, so a job that kills its worker every time it runs (a file that panics a parser
// below the recover, or exhausts memory) is failed on its next claim instead of being handed
// out first again on every restart, stalling the queue for every account behind it.
const maxAttempts = 3

// demoPurgeInterval is far coarser than pollInterval — expired demo accounts (business_
// plan.md §8.2, demo_purge.go) are bounded by demoSessionTTL (hours), not something that
// needs sub-second responsiveness the way the job queue does.
const demoPurgeInterval = 5 * time.Minute

// accountPurgeInterval is how long a deleted account's data can outlast the request that
// deleted it (account_purge.go), plus however long a job of its takes to finish.
const accountPurgeInterval = time.Minute

// heatmapAgingInterval is daily, not weekly — heatmap_aging.go's sweep is cheap (one indexed
// query plus whatever small number of activities actually crossed the window boundary since
// the last run), so there's no reason to let staleness accumulate to a week when a day is just
// as easy to check.
const heatmapAgingInterval = 24 * time.Hour

// heatmapCapInterval matches heatmapAgingInterval's own reasoning — recomputeHeatmapCaps
// (heatmap_cap.go) is one cheap indexed query per user with any in-window activity, so daily
// is easy to afford and keeps users.heatmap_cap from drifting far from an account's actual
// coverage between sweeps.
const heatmapCapInterval = 24 * time.Hour

type job struct {
	id      int64
	kind    string
	payload []byte
}

// exportSweepInterval is how often expired exports are removed (export_job.go).
const exportSweepInterval = time.Hour

// Run polls until ctx is cancelled. Each job is claimed in its own short transaction (FOR
// UPDATE SKIP LOCKED, then locked_at stamped), run outside it, and marked done/failed after.
// The fresh locked_at, not the row lock, is what keeps a second worker process off a job
// that is still running (claimLease).
//
// Long jobs have a lane of their own, a second loop beside this one: building an export or
// unpacking a large archive takes minutes, and nobody's single upload or phone sync should
// wait behind it. The main lane itself runs as `concurrency` loops side by side, fair across
// accounts (claimQuery). n tells an export's owner when it's ready (export_job.go). The periodic
// sweeps (runSweeps) have a third goroutine, so a busy queue never holds them back. A deleted
// account's jobs are never claimed: its purge drops them within accountPurgeInterval.
func Run(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, n *Notifier, concurrency int) error {
	notifier = n
	var lanes sync.WaitGroup
	defer lanes.Wait() // a long job cut off by shutdown is released before Run returns
	lanes.Add(1)
	go func() {
		defer lanes.Done()
		t := time.NewTicker(longPollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := claimAndRun(ctx, pool, store, log, laneLong); err != nil {
					log.Error("export job processing error", "err", err)
				}
			}
		}
	}()

	lanes.Add(1)
	go func() {
		defer lanes.Done()
		runSweeps(ctx, pool, store, log)
	}()

	for range concurrency {
		lanes.Add(1)
		go func() {
			defer lanes.Done()
			runMainLane(ctx, pool, store, log)
		}()
	}
	<-ctx.Done()
	return nil
}

// runMainLane is one of Run's `concurrency` main-lane loops: claim and run jobs until the
// queue has nothing this loop may take (claimQuery: one job per account at a time), then wait
// for the next tick.
func runMainLane(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				processed, err := claimAndRunOne(ctx, pool, store, log)
				if err != nil {
					log.Error("job processing error", "err", err)
					break
				}
				if !processed {
					break // nothing claimable; wait for the next tick
				}
			}
		}
	}
}

// runSweeps runs the periodic sweeps until ctx is cancelled. It has a goroutine of its own
// rather than a case in Run's job loop: that loop drains the whole queue before it selects
// again, so a queue that never empties (a big import) would hold back a deleted account's
// purge, the demo purge and the daily sweeps for as long as it lasts.
func runSweeps(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) {
	demoTicker := time.NewTicker(demoPurgeInterval)
	defer demoTicker.Stop()
	accountTicker := time.NewTicker(accountPurgeInterval)
	defer accountTicker.Stop()
	heatmapTicker := time.NewTicker(heatmapAgingInterval)
	defer heatmapTicker.Stop()
	heatmapCapTicker := time.NewTicker(heatmapCapInterval)
	defer heatmapCapTicker.Stop()
	exportSweepTicker := time.NewTicker(exportSweepInterval)
	defer exportSweepTicker.Stop()

	// A ticker's first tick is a whole interval away, so the daily sweeps also run once at
	// start: a worker restarted more often than daily (every deploy) would otherwise never
	// reach them.
	if err := ageOutHeatmapWindow(ctx, pool, log); err != nil {
		log.Error("heatmap aging error", "err", err)
	}
	if err := recomputeHeatmapCaps(ctx, pool, log); err != nil {
		log.Error("heatmap cap error", "err", err)
	}
	if err := sweepExports(ctx, pool, store, log); err != nil {
		log.Error("export sweep error", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-demoTicker.C:
			if err := purgeExpiredDemoUsers(ctx, pool, store, log); err != nil {
				log.Error("demo purge error", "err", err)
			}
		case <-accountTicker.C:
			if err := purgeDeletedAccounts(ctx, pool, store, log); err != nil {
				log.Error("account purge error", "err", err)
			}
		case <-heatmapTicker.C:
			if err := ageOutHeatmapWindow(ctx, pool, log); err != nil {
				log.Error("heatmap aging error", "err", err)
			}
		case <-exportSweepTicker.C:
			if err := sweepExports(ctx, pool, store, log); err != nil {
				log.Error("export sweep error", "err", err)
			}
		case <-heatmapCapTicker.C:
			if err := recomputeHeatmapCaps(ctx, pool, log); err != nil {
				log.Error("heatmap cap error", "err", err)
			}
		}
	}
}

// The two lanes Run claims from: the long-running kinds (an export, unpacking an archive) and
// everything else.
const (
	laneMain = false
	laneLong = true
)

// heartbeat refreshes a long job's claim: a big account's export or a big archive can take
// longer than claimLease, and a job that looked abandoned would be claimed and run a second
// time.
const heartbeat = 5 * time.Minute

// keepClaimed refreshes jobID's locked_at every heartbeat until the returned stop is called.
func keepClaimed(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, jobID int64) (stop func()) {
	hctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-hctx.Done():
				return
			case <-t.C:
				if _, err := pool.Exec(hctx, `UPDATE jobs SET locked_at = NOW() WHERE id = $1`, jobID); err != nil && hctx.Err() == nil {
					log.Error("job heartbeat failed", "job_id", jobID, "err", err)
				}
			}
		}
	}()
	return cancel
}

// claimAndRunOne is claimAndRun on the main lane.
func claimAndRunOne(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) (bool, error) {
	return claimAndRun(ctx, pool, store, log, laneMain)
}

// claimQuery picks the lane's next job, fairly across accounts:
//
//   - One job per account at a time. An account with a job of this lane already claimed
//     (busy) waits, which keeps its own jobs in order (an edit, then its render) and stops
//     one account's import from taking every worker.
//   - Of each other account, only its next job (runnable, by run_after then id) is a
//     candidate, and the candidates go in order of when their account was last served (its
//     latest claim on this lane, idx_jobs_user_locked), never-served first. A new user's single
//     upload therefore goes ahead of the thousandth file of someone else's import, instead of
//     after it.
//   - A deleted account's jobs are never claimed: its purge drops them (account_purge.go).
//
// The outer query repeats the claimability checks on jobs itself, not only in the CTE: under
// READ COMMITTED, FOR UPDATE re-checks just those conditions against a row another worker
// claimed and committed after this statement's snapshot, so they are what keeps two workers
// off the same job. SKIP LOCKED then moves on to the next candidate, another account's.
const claimQuery = `
	WITH busy AS (
		SELECT DISTINCT user_id FROM jobs
		WHERE state = 'pending' AND user_id IS NOT NULL
		  AND locked_at >= NOW() - make_interval(secs => $1)
		  AND (kind IN ('export', 'unpack')) = $2
	),
	heads AS (
		SELECT DISTINCT ON (j.user_id) j.id, j.user_id, j.run_after
		FROM jobs j
		WHERE j.state = 'pending' AND j.run_after <= NOW()
		  AND (j.locked_at IS NULL OR j.locked_at < NOW() - make_interval(secs => $1))
		  AND (j.kind IN ('export', 'unpack')) = $2
		  AND (j.user_id IS NULL OR j.user_id NOT IN (SELECT user_id FROM busy))
		  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = j.user_id AND u.deleted_at IS NOT NULL)
		ORDER BY j.user_id, j.run_after, j.id
	),
	ranked AS (
		SELECT h.id, h.run_after,
			(SELECT max(s.locked_at) FROM jobs s
			 WHERE s.user_id = h.user_id AND (s.kind IN ('export', 'unpack')) = $2) AS served
		FROM heads h
	)
	SELECT j.id, j.kind, j.payload, j.attempts
	FROM jobs j JOIN ranked r ON r.id = j.id
	WHERE j.state = 'pending'
	  AND (j.locked_at IS NULL OR j.locked_at < NOW() - make_interval(secs => $1))
	ORDER BY r.served NULLS FIRST, r.run_after, r.id
	FOR UPDATE OF j SKIP LOCKED
	LIMIT 1`

func claimAndRun(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, long bool) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("worker: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	var j job
	var attempts int
	err = tx.QueryRow(ctx, claimQuery, claimLease.Seconds(), long).Scan(&j.id, &j.kind, &j.payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("worker: claim: %w", err)
	}

	if attempts >= maxAttempts {
		// Every earlier claim ended without the job being marked done or failed: the worker
		// running it died each time.
		log.Error("job abandoned", "job_id", j.id, "kind", j.kind, "attempts", attempts)
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET state = 'failed', last_error = $2, error_code = $3, finished_at = NOW()
			WHERE id = $1
		`, j.id, fmt.Sprintf("worker stopped while running this job %d times", attempts), failureCode(j.kind, nil)); err != nil {
			return false, fmt.Errorf("worker: mark abandoned: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("worker: commit abandoned: %w", err)
		}
		finished(j.kind, "failed", failureCode(j.kind, nil))
		return true, nil
	}

	if _, err := tx.Exec(ctx, `UPDATE jobs SET locked_at = NOW(), attempts = attempts + 1 WHERE id = $1`, j.id); err != nil {
		return false, fmt.Errorf("worker: mark locked: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("worker: commit claim: %w", err)
	}

	start := time.Now()
	runErr := runJobSafely(ctx, pool, store, log, j)
	metrics.JobDuration.WithLabelValues(j.kind).Observe(time.Since(start).Seconds())

	// The rest records the outcome on a context that outlives a shutdown: a job that finished
	// as SIGTERM arrived is still done, and one cut off by it is still owed a release.
	wctx := context.WithoutCancel(ctx)

	// The worker is shutting down (a deploy, SIGTERM): the job didn't fail, it was cut off.
	// Hand it back for the next start rather than failing it or leaving it locked for a whole
	// lease.
	if runErr != nil && ctx.Err() != nil {
		_, uerr := pool.Exec(wctx, `
			UPDATE jobs SET locked_at = NULL, attempts = attempts - 1 WHERE id = $1 AND state = 'pending'
		`, j.id)
		if uerr != nil {
			return true, fmt.Errorf("worker: release on shutdown: %w", uerr)
		}
		return true, nil
	}

	if runErr != nil {
		log.Error("job failed", "job_id", j.id, "kind", j.kind, "err", runErr)
		code := failureCode(j.kind, runErr)
		_, uerr := pool.Exec(wctx, `
			UPDATE jobs SET state = 'failed', last_error = $2, error_code = $3, finished_at = NOW()
			WHERE id = $1
		`, j.id, runErr.Error(), code)
		if uerr != nil {
			return true, fmt.Errorf("worker: mark failed: %w", uerr)
		}
		finished(j.kind, "failed", code)
		return true, nil // the queue made progress even though this job failed
	}

	if _, err := pool.Exec(wctx, `UPDATE jobs SET state = 'done', finished_at = NOW() WHERE id = $1`, j.id); err != nil {
		return true, fmt.Errorf("worker: mark done: %w", err)
	}
	log.Info("job done", "job_id", j.id, "kind", j.kind)
	finished(j.kind, "done", nil)
	return true, nil
}

// finished counts a job in metrics.JobsFinished. A failure with no jobs.error_code (any kind
// but ingest and unpack) counts as "internal": only their codes can name the user's file.
func finished(kind, outcome string, code *string) {
	c := ""
	if outcome == "failed" {
		c = ingest.FailInternal
		if code != nil {
			c = *code
		}
	}
	metrics.JobsFinished.WithLabelValues(kind, outcome, c).Inc()
}

// failureCode is the error_code a failed job is stored with. Only an ingest's or an unpack's
// failure reaches a person (the upload history, the Upload menu), so only they get a code to
// be translated from; the other kinds' failures are for the logs. A nil err is a failure that isn't the file's own
// (the abandoned-job path), so FailInternal.
func failureCode(kind string, err error) *string {
	var c string
	switch {
	case kind == "unpack" && err != nil:
		c = unpack.FailureCode(err)
	case kind == "unpack":
		c = unpack.FailInternal
	case kind != "ingest":
		return nil
	case err != nil:
		c = ingest.FailureCode(err)
	default:
		c = ingest.FailInternal
	}
	return &c
}

// runJobSafely is runJob with a panic turned into the job's error. One malformed file must
// fail its own job, not take the worker process down with it — a crash would leave the job
// claimed and hand it straight back out on restart.
func runJobSafely(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, j job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("job panicked", "job_id", j.id, "kind", j.kind, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return jobRunner(ctx, pool, store, log, j)
}

// jobRunner is runJob; a test swaps in a job that blocks until shutdown.
var jobRunner = runJob

func runJob(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, j job) error {
	switch j.kind {
	case "ingest":
		var ij ingest.Job
		if err := json.Unmarshal(j.payload, &ij); err != nil {
			return fmt.Errorf("unmarshal ingest job: %w", err)
		}
		if err := ingest.PromoteRaw(ctx, pool, store, j.id, ij.RawPayloadKey); err != nil {
			return err
		}
		res, err := ingest.Process(ctx, pool, store, ij)
		if err != nil {
			return err
		}
		if !res.Persisted {
			return nil // idempotency requirement: an already-processed duplicate is a success
		}
		return nil
	case "render_fog":
		var rj ingest.RenderFogJob
		if err := json.Unmarshal(j.payload, &rj); err != nil {
			return fmt.Errorf("unmarshal render_fog job: %w", err)
		}
		return fog.RenderUser(ctx, pool, store, rj.UserID)
	case "edit_track":
		var ej ingest.EditJob
		if err := json.Unmarshal(j.payload, &ej); err != nil {
			return fmt.Errorf("unmarshal edit_track job: %w", err)
		}
		return ingest.ProcessTrackEdit(ctx, pool, store, ej)
	case "reprivacy":
		var rj ingest.ReprivacyJob
		if err := json.Unmarshal(j.payload, &rj); err != nil {
			return fmt.Errorf("unmarshal reprivacy job: %w", err)
		}
		return ingest.ProcessReprivacy(ctx, pool, store, j.id, rj)
	case "story_copy":
		var sj storycopy.Job
		if err := json.Unmarshal(j.payload, &sj); err != nil {
			return fmt.Errorf("unmarshal story_copy job: %w", err)
		}
		return storycopy.Process(ctx, pool, store, sj)
	case "export":
		return runExport(ctx, pool, store, log, j.id, j.payload)
	case "unpack":
		defer keepClaimed(ctx, pool, log, j.id)()
		return unpack.Run(ctx, pool, store, j.id, j.payload)
	default:
		return fmt.Errorf("unhandled job kind %q (export / provider_sync / retention are out of scope for this task)", j.kind)
	}
}
