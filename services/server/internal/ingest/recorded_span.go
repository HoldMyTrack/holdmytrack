package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parallel"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// recordedSpan is the stretch of time a recording, or the split piece rng narrows it to, covers
// as recorded: before Private locations clip its ends and before any track edit. It's what the
// hint compares an activity by (recorded_started_at, recorded_ended_at), since another source's
// copy of the same walk still covers the minutes spent inside a Private location. Nil when the
// piece has no points.
func recordedSpan(points []parse.Point, rng SplitRange) (start, end *time.Time) {
	piece := rng.Apply(points, nil)
	if len(piece) == 0 {
		return nil, nil
	}
	return &piece[0].Time, &piece[len(piece)-1].Time
}

// BackfillRecordedSpans fills recorded_started_at and recorded_ended_at (§4.6) for every
// activity stored before ingest set them, from its raw payload. One-off, run once on deploying
// migration 0033 (`backfill-recorded-spans`); idempotent, since it only reads rows still
// missing them, and a reprocess that sets them meanwhile wins. An activity whose payload can't
// be read is logged and left to the clipped-track fallback rather than failing the run.
func BackfillRecordedSpans(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	type row struct {
		id, sourceDetail, rawKey string
		rng                      SplitRange
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, COALESCE(source_detail, ''), raw_payload_key, split_from, split_to
		FROM activities
		WHERE recorded_started_at IS NULL AND raw_payload_key IS NOT NULL
		ORDER BY user_id, started_at
	`)
	if err != nil {
		return fmt.Errorf("list activities: %w", err)
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.sourceDetail, &r.rawKey, &r.rng.From, &r.rng.To); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var filled, skipped atomic.Int64
	err = parallel.ForEach(ctx, len(todo), rerenderParallelism, func(ctx context.Context, i int) error {
		r := todo[i]
		act, err := loadRecorded(ctx, store, r.sourceDetail, r.rawKey)
		if err != nil {
			log.Warn("backfill-recorded-spans: skipped", "activity", r.id, "err", err)
			skipped.Add(1)
			return nil
		}
		start, end := recordedSpan(act.Points, r.rng)
		if start == nil {
			skipped.Add(1)
			return nil
		}
		if _, err := pool.Exec(ctx, `
			UPDATE activities SET recorded_started_at = $2, recorded_ended_at = $3
			WHERE id = $1 AND recorded_started_at IS NULL
		`, r.id, *start, *end); err != nil {
			return fmt.Errorf("update %s: %w", r.id, err)
		}
		filled.Add(1)
		return nil
	})
	if err != nil {
		return err
	}
	log.Info("backfill-recorded-spans: done", "filled", filled.Load(), "skipped", skipped.Load())
	return nil
}
