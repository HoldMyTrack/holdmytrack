package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// recomputeHeatmapCaps is what keeps each account's users.heatmap_cap (migrations/
// 0019_heatmap_cap.sql) matching its own actual coverage, since nothing about an account's
// touch-count distribution changes on an event a request handler could hook — it drifts
// gradually as activities are ingested, deleted, or age out of the heatmap window
// (heatmap_aging.go, run independently — whichever of the two runs first on a given day, the
// other self-corrects on its own next tick from whatever in_heatmap_window state exists by
// then, so there's no ordering dependency between them).
//
// Unlike demo_purge.go's sweep, there is no LIMIT/batch here: fog.RecomputeHeatmapCap's own
// query per user is cheap (one indexed GROUP BY), and a LIMIT would just starve whichever
// users sort last, since — unlike expired demo accounts — recomputing a cap never removes a
// user from tomorrow's candidate set.
func recomputeHeatmapCaps(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT user_id FROM activities WHERE in_heatmap_window AND superseded_by IS NULL
	`)
	if err != nil {
		return fmt.Errorf("heatmap cap: query candidate users: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("heatmap cap: scan: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("heatmap cap: rows: %w", err)
	}

	changedCount := 0
	for _, userID := range userIDs {
		newCap, changed, err := fog.RecomputeHeatmapCap(ctx, pool, userID)
		if err != nil {
			log.Error("heatmap cap: recompute failed", "user_id", userID, "err", err)
			continue
		}
		if !changed {
			continue
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET heatmap_cap = $2 WHERE id = $1`, userID, newCap); err != nil {
			log.Error("heatmap cap: update failed", "user_id", userID, "err", err)
			continue
		}
		// Every z14 tile the account has ever touched already has a fog_tiles row (it's
		// created the first time an activity touches it) — marking all of them dirty, rather
		// than recomputing the account's full touched-tile list again, is simpler and exactly
		// as correct.
		if _, err := pool.Exec(ctx, `UPDATE fog_tiles SET dirty = true WHERE user_id = $1`, userID); err != nil {
			log.Error("heatmap cap: mark dirty failed", "user_id", userID, "err", err)
			continue
		}
		if err := ingest.EnqueueRenderFog(ctx, pool, userID); err != nil {
			log.Error("heatmap cap: enqueue render failed", "user_id", userID, "err", err)
			continue
		}
		changedCount++
	}
	log.Info("heatmap cap: swept", "users_considered", len(userIDs), "users_changed", changedCount)
	return nil
}
