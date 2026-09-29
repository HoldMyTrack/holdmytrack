package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/spots"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// matchSpots replaces an activity's Spots visits (spots.Match) from its final points — after
// the Private location clip and any track edit, and after its trajectory is persisted. The
// clip only trims a track's ends, so a point a track passes through a Private location with is
// still in points; it's flagged here, and never counts towards a stay.
func matchSpots(ctx context.Context, pool *pgxpool.Pool, userID, activityID string, points []parse.Point) error {
	zones, err := LoadZones(ctx, pool, userID)
	if err != nil {
		return fmt.Errorf("load private locations: %w", err)
	}
	flagged := make([]spots.Point, len(points))
	for i, p := range points {
		flagged[i] = spots.Point{Point: p, Private: insideAny(zones, p)}
	}
	return spots.Match(ctx, pool, userID, activityID, flagged)
}

// BackfillSpotVisits is the `match_spots` job, queued by the Spots import: it matches every
// activity whose trajectory crosses any spot, from the same points the activity's own
// processing would use (DisplayedPoints). An activity with an edit or a Private location change
// still pending is skipped — its reprocess matches it when it lands. One activity failing
// doesn't stop the rest; the job fails afterwards, naming them. Every account's tile version is
// bumped at the end, so the visited marks show.
func BackfillSpotVisits(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `
		SELECT a.id, a.user_id FROM activities a
		WHERE a.trajectory IS NOT NULL AND a.raw_payload_key IS NOT NULL AND NOT a.edit_pending
		  AND EXISTS (SELECT 1 FROM spots s WHERE ST_Intersects(s.geom, a.trajectory))
	`)
	if err != nil {
		return fmt.Errorf("match_spots: list activities: %w", err)
	}
	type candidate struct{ activityID, userID string }
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate
		return c, row.Scan(&c.activityID, &c.userID)
	})
	if err != nil {
		return fmt.Errorf("match_spots: list activities: %w", err)
	}
	log.Info("match_spots: starting", "activities", len(candidates))

	var failed []string
	for i, c := range candidates {
		points, err := DisplayedPoints(ctx, pool, store, c.activityID)
		if err == nil {
			err = matchSpots(ctx, pool, c.userID, c.activityID, points)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", c.activityID, err))
		}
		if (i+1)%500 == 0 {
			log.Info("match_spots: progress", "done", i+1, "of", len(candidates))
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
		return fmt.Errorf("match_spots: bump tile versions: %w", err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("match_spots: %d of %d activities failed: %s", len(failed), len(candidates), strings.Join(failed, "; "))
	}
	return nil
}
