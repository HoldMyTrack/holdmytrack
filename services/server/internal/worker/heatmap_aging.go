package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// ageOutHeatmapWindow is what slides Heatmap's rolling window (fog.HeatmapWindowDays) forward
// for activities nothing else has a reason to touch — ingest and delete already mark the
// tiles they change dirty and re-render immediately, but an activity that simply gets older
// needs something to notice that on the calendar's own schedule, not an event.
//
// Deliberately per-activity, not per-user: activities.in_heatmap_window (migrations/
// 0002_activities.sql) turns "is this activity still within the window" into
// a stored fact this query can select directly, and each one that just crossed the boundary
// only ever needs *its own* tiles re-rendered (ActivityTiles, the same lookup
// handleDeleteActivity's activityFogTiles uses) — never a full account-wide rebuild the way an
// earlier version of this swept every tile for every user on the same weekly tick. Because
// activities age out on whatever calendar day happens to be 365 days after they were created,
// which were already scattered across the year, this naturally spreads across days on its own
// — no bucketing or staggering needed to avoid a thundering herd.
func ageOutHeatmapWindow(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	cutoff := time.Now().AddDate(0, 0, -fog.HeatmapWindowDays)
	rows, err := pool.Query(ctx, `
		SELECT id, user_id FROM activities
		WHERE in_heatmap_window AND superseded_by IS NULL AND started_at < $1
	`, cutoff)
	if err != nil {
		return fmt.Errorf("heatmap aging: query aged activities: %w", err)
	}
	type agedActivity struct{ id, userID string }
	var aged []agedActivity
	for rows.Next() {
		var a agedActivity
		if err := rows.Scan(&a.id, &a.userID); err != nil {
			rows.Close()
			return fmt.Errorf("heatmap aging: scan: %w", err)
		}
		aged = append(aged, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("heatmap aging: rows: %w", err)
	}

	// One render_fog job per user touched this sweep, not one per activity — several aged-out
	// activities landing on the same day for the same user coalesce into a single render pass,
	// same reasoning as EnqueueRenderFog's own doc comment.
	touchedUsers := map[string]struct{}{}
	for _, a := range aged {
		tiles, err := ingest.ActivityTiles(ctx, pool, a.id)
		if err != nil {
			log.Error("heatmap aging: tile lookup failed", "activity_id", a.id, "err", err)
			continue
		}
		if _, err := pool.Exec(ctx, `UPDATE activities SET in_heatmap_window = false WHERE id = $1`, a.id); err != nil {
			log.Error("heatmap aging: flag update failed", "activity_id", a.id, "err", err)
			continue
		}
		if len(tiles) > 0 {
			if err := ingest.MarkFogTilesDirty(ctx, pool, a.userID, tiles); err != nil {
				log.Error("heatmap aging: mark dirty failed", "activity_id", a.id, "err", err)
				continue
			}
		}
		touchedUsers[a.userID] = struct{}{}
	}

	for userID := range touchedUsers {
		if err := ingest.EnqueueRenderFog(ctx, pool, userID); err != nil {
			log.Error("heatmap aging: enqueue render failed", "user_id", userID, "err", err)
		}
	}
	log.Info("heatmap aging: swept", "activities", len(aged), "users", len(touchedUsers))
	return nil
}
