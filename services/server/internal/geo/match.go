package geo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MatchActivity records which countries/regions activityID's trajectory touches, called once
// from ingest.Process right after the activity row is persisted — both queries below read
// activities.trajectory back by id rather than taking geometry as a parameter, so nothing
// upstream needs to hold onto it for this.
//
// Matches against the already-persisted display trajectory, not the raw pre-simplification
// points fog masks use: ingest's ~3 m simplification tolerance is well inside the outlines' own
// accuracy, and reading the column back avoids a second geometry pass in Go. It tests the
// full-detail outlines' pieces (admin_country_parts/admin_region_parts), never the simplified
// ones the tiles draw. Every polygon touched gets a row, however
// briefly — matching the product requirement that a visit's size or duration doesn't matter,
// only whether it happened.
//
// Not filtered by superseded_by: activity_tile_masks keeps rows for a superseded duplicate
// too (so a later-deleted winner makes the loser's own coverage live again for free), and
// this mirrors that. The country/region "unlocked" tile queries do their own superseded_by
// filtering at read time instead.
func MatchActivity(ctx context.Context, pool *pgxpool.Pool, activityID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO activity_country (activity_id, country_id)
		SELECT DISTINCT $1::uuid, p.country_id FROM admin_country_parts p
		WHERE ST_Intersects(p.geom, (SELECT trajectory FROM activities WHERE id = $1))
		ON CONFLICT DO NOTHING
	`, activityID)
	if err != nil {
		return fmt.Errorf("geo: match countries: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO activity_region (activity_id, region_id)
		SELECT DISTINCT $1::uuid, p.region_id FROM admin_region_parts p
		WHERE ST_Intersects(p.geom, (SELECT trajectory FROM activities WHERE id = $1))
		ON CONFLICT DO NOTHING
	`, activityID)
	if err != nil {
		return fmt.Errorf("geo: match regions: %w", err)
	}
	return nil
}
