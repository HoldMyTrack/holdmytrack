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
// points fog masks use: admin polygons are kilometers across, so ingest's ~3 m simplification
// tolerance cannot plausibly change which one a segment intersects, and reading the column
// back avoids a second geometry pass in Go. Every polygon touched gets a row, however
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
		SELECT $1, c.id FROM admin_countries c
		WHERE ST_Intersects(c.geom, (SELECT trajectory FROM activities WHERE id = $1))
		ON CONFLICT DO NOTHING
	`, activityID)
	if err != nil {
		return fmt.Errorf("geo: match countries: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO activity_region (activity_id, region_id)
		SELECT $1, r.id FROM admin_regions r
		WHERE ST_Intersects(r.geom, (SELECT trajectory FROM activities WHERE id = $1))
		ON CONFLICT DO NOTHING
	`, activityID)
	if err != nil {
		return fmt.Errorf("geo: match regions: %w", err)
	}
	return nil
}
