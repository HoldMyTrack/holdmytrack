package fog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StoreHeatPointCaps records, for every zoom up to HeatActivityMaxZoom, the most in-window,
// non-Pending activities any one heat-points cell holds (users.heat_point_caps, index z+1): what
// each of those zooms' weights are measured against (HeatScale), so the busiest place reads
// hottest at every zoom. Stored by every render, which follows every change to the account's
// coverage, so a tile request reads it rather than grouping the whole account each time.
func StoreHeatPointCaps(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	_, err := pool.Exec(ctx, `
		WITH acts AS MATERIALIZED (
			SELECT id FROM activities WHERE user_id = $1 AND in_heatmap_window AND NOT edit_pending
		), per_cell AS (
			SELECT z.z, count(DISTINCT m.activity_id) AS n
			FROM acts JOIN activity_tile_masks m ON m.activity_id = acts.id AND m.zoom = $2
			CROSS JOIN generate_series(0, $3::int) AS z(z)
			GROUP BY z.z, m.tile_x >> ($3 - z.z), m.tile_y >> ($3 - z.z)
		)
		UPDATE users SET heat_point_caps = (
			SELECT array_agg(c ORDER BY z) FROM (SELECT z, max(n) AS c FROM per_cell GROUP BY z) t
		)
		WHERE id = $1`, userID, Zoom, HeatActivityMaxZoom)
	if err != nil {
		return fmt.Errorf("fog: store heat point caps: %w", err)
	}
	return nil
}
