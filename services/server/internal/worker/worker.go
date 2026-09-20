// Package worker is cmd/fitmap work: dequeues jobs with FOR UPDATE SKIP LOCKED
// (IMPLEMENTATION.md §3.8, §4.1) and runs internal/ingest for `ingest` jobs.
// No broker, per §1.1 — this poll loop against idx_jobs_runnable is the whole queue.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/fog"
	"github.com/fitmap/fitmap/services/server/internal/ingest"
	"github.com/fitmap/fitmap/services/server/internal/storage"
)

const pollInterval = 500 * time.Millisecond

// demoPurgeInterval is far coarser than pollInterval — expired demo accounts (business_
// plan.md §8.2, demo_purge.go) are bounded by demoSessionTTL (hours), not something that
// needs sub-second responsiveness the way the job queue does.
const demoPurgeInterval = 5 * time.Minute

// heatmapRefreshInterval matches fog.HeatmapWindowDays' own granularity, not the job queue's
// — an activity aging out of the rolling window is a once-a-week-at-most event for any given
// tile, so there is nothing to gain from checking more often (heatmap_refresh.go's own
// comment explains why weekly, not live, is the right cadence for this at all).
const heatmapRefreshInterval = 7 * 24 * time.Hour

type job struct {
	id      int64
	kind    string
	payload []byte
}

// Run polls until ctx is cancelled. Each job is claimed, run, and marked done/failed in its
// own transaction so FOR UPDATE SKIP LOCKED lets multiple worker processes share the queue
// safely — not exercised by this task's single-worker verification, but the point of using
// SKIP LOCKED at all rather than a plain SELECT ... FOR UPDATE.
func Run(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	demoTicker := time.NewTicker(demoPurgeInterval)
	defer demoTicker.Stop()
	heatmapTicker := time.NewTicker(heatmapRefreshInterval)
	defer heatmapTicker.Stop()

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
			if err := refreshHeatmapWindows(ctx, pool, log); err != nil {
				log.Error("heatmap refresh error", "err", err)
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
	err = tx.QueryRow(ctx, `
		SELECT id, kind, payload FROM jobs
		WHERE state = 'pending' AND run_after <= NOW()
		ORDER BY run_after, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&j.id, &j.kind, &j.payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("worker: claim: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE jobs SET locked_at = NOW() WHERE id = $1`, j.id); err != nil {
		return false, fmt.Errorf("worker: mark locked: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("worker: commit claim: %w", err)
	}

	runErr := runJob(ctx, pool, store, j)

	if runErr != nil {
		log.Error("job failed", "job_id", j.id, "kind", j.kind, "err", runErr)
		_, uerr := pool.Exec(ctx, `
			UPDATE jobs SET state = 'failed', attempts = attempts + 1, last_error = $2
			WHERE id = $1
		`, j.id, runErr.Error())
		if uerr != nil {
			return true, fmt.Errorf("worker: mark failed: %w", uerr)
		}
		return true, nil // the queue made progress even though this job failed
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET state = 'done' WHERE id = $1`, j.id); err != nil {
		return true, fmt.Errorf("worker: mark done: %w", err)
	}
	log.Info("job done", "job_id", j.id, "kind", j.kind)
	return true, nil
}

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
	default:
		return fmt.Errorf("unhandled job kind %q (export / reprivacy / provider_sync / retention are out of scope for this task)", j.kind)
	}
}
