package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/ingest"
)

// refreshHeatmapWindows is what actually slides Heatmap's rolling window (fog.HeatmapWindowDays)
// forward for accounts with no new activity to trigger a re-render on its own — ingest and
// delete already mark the tiles they touch dirty and re-render immediately (internal/ingest's
// MarkFogTilesDirty/EnqueueRenderFog), but an activity that simply ages past the window needs
// something to notice on the calendar's own schedule, not an event. Marking every tile dirty
// and re-rendering weekly, rather than tracking each tile's exact "oldest contributing
// activity ages out on this date," trades a few days of staleness at the trailing edge for a
// much simpler mechanism — reusing internal/fog.RenderUser exactly as ingest already does,
// with no new rendering path of its own. Fog's own aggregate has no aging concept, so
// recomputing it here every week is pure, harmless duplicate work, not a correctness risk.
func refreshHeatmapWindows(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `SELECT DISTINCT user_id FROM fog_tiles`)
	if err != nil {
		return fmt.Errorf("heatmap refresh: query users: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("heatmap refresh: scan: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("heatmap refresh: rows: %w", err)
	}

	for _, userID := range userIDs {
		if err := ingest.MarkAllFogTilesDirty(ctx, pool, userID); err != nil {
			log.Error("heatmap refresh: mark dirty failed", "user_id", userID, "err", err)
			continue
		}
		if err := ingest.EnqueueRenderFog(ctx, pool, userID); err != nil {
			log.Error("heatmap refresh: enqueue render failed", "user_id", userID, "err", err)
			continue
		}
	}
	log.Info("heatmap refresh: swept users", "count", len(userIDs))
	return nil
}
