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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
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

// Run polls until ctx is cancelled. Each job is claimed in its own short transaction (FOR
// UPDATE SKIP LOCKED, then locked_at stamped), run outside it, and marked done/failed after.
// The fresh locked_at, not the row lock, is what keeps a second worker process off a job
// that is still running (claimLease).
func Run(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	demoTicker := time.NewTicker(demoPurgeInterval)
	defer demoTicker.Stop()
	heatmapTicker := time.NewTicker(heatmapAgingInterval)
	defer heatmapTicker.Stop()
	heatmapCapTicker := time.NewTicker(heatmapCapInterval)
	defer heatmapCapTicker.Stop()

	// A ticker's first tick is a whole interval away, so the daily sweeps also run once at
	// start: a worker restarted more often than daily (every deploy) would otherwise never
	// reach them.
	if err := ageOutHeatmapWindow(ctx, pool, log); err != nil {
		log.Error("heatmap aging error", "err", err)
	}
	if err := recomputeHeatmapCaps(ctx, pool, log); err != nil {
		log.Error("heatmap cap error", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				processed, err := claimAndRunOne(ctx, pool, store, log)
				if err != nil {
					log.Error("job processing error", "err", err)
					break
				}
				if !processed {
					break // queue empty; wait for the next tick
				}
			}
		case <-demoTicker.C:
			if err := purgeExpiredDemoUsers(ctx, pool, store, log); err != nil {
				log.Error("demo purge error", "err", err)
			}
		case <-heatmapTicker.C:
			if err := ageOutHeatmapWindow(ctx, pool, log); err != nil {
				log.Error("heatmap aging error", "err", err)
			}
		case <-heatmapCapTicker.C:
			if err := recomputeHeatmapCaps(ctx, pool, log); err != nil {
				log.Error("heatmap cap error", "err", err)
			}
		}
	}
}

func claimAndRunOne(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("worker: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	var j job
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT id, kind, payload, attempts FROM jobs
		WHERE state = 'pending' AND run_after <= NOW()
		  AND (locked_at IS NULL OR locked_at < NOW() - make_interval(secs => $1))
		ORDER BY run_after, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, claimLease.Seconds()).Scan(&j.id, &j.kind, &j.payload, &attempts)
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
// but ingest) counts as "internal": only an ingest's codes can name the user's file.
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

// failureCode is the error_code a failed job is stored with. Only an ingest job's failure
// reaches a person (the upload history), so only it gets a code to be translated from; the
// other kinds' failures are for the logs. A nil err is a failure that isn't the file's own
// (the abandoned-job path), so FailInternal.
func failureCode(kind string, err error) *string {
	if kind != "ingest" {
		return nil
	}
	c := ingest.FailInternal
	if err != nil {
		c = ingest.FailureCode(err)
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
	return jobRunner(ctx, pool, store, j)
}

// jobRunner is runJob; a test swaps in a job that blocks until shutdown.
var jobRunner = runJob

func runJob(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, j job) error {
	switch j.kind {
	case "ingest":
		var ij ingest.Job
		if err := json.Unmarshal(j.payload, &ij); err != nil {
			return fmt.Errorf("unmarshal ingest job: %w", err)
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
	default:
		return fmt.Errorf("unhandled job kind %q (export / provider_sync / retention are out of scope for this task)", j.kind)
	}
}
