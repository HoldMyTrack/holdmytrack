// Package spots is Spots (IMPLEMENTATION.md §3.20, §4.25, ADR-0021): outdoor places imported
// once from OpenStreetMap (import.go), and the visits an activity makes to them — five
// minutes or more inside a spot's area, worked out from the activity's own points (this file).
package spots

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// MinDwellSeconds is how long an activity has to spend inside a spot for it to count as
// visited: long enough that driving past or running along a park's edge doesn't, short
// enough that a stop at a viewpoint does (ADR-0021).
const MinDwellSeconds = 5 * 60

// Point is one of an activity's points as matching sees it. Private is a point inside one of
// the account's Private locations: never counted, and it breaks a stay in two, so a spot
// inside a Private location is never visited even by a track that only passes through it.
type Point struct {
	parse.Point
	Private bool
}

// Match replaces activityID's visits with the ones its points make now. Candidates are the
// spots its stored display trajectory intersects (an indexed lookup); for each, the time
// between consecutive points that are both inside is summed, and MinDwellSeconds or more is a
// visit. The trajectory is only the candidate filter — the dwell is summed over the full,
// unsimplified points, which the caller already has in memory.
//
// The caller persists the trajectory before calling this. Nil or too few points clears the
// activity's visits: an activity hidden entirely by Private locations visits nothing.
func Match(ctx context.Context, pool *pgxpool.Pool, userID, activityID string, points []Point) error {
	idx := make([]int32, 0, len(points))
	lons := make([]float64, 0, len(points))
	lats := make([]float64, 0, len(points))
	ts := make([]float64, 0, len(points))
	for i, p := range points {
		if p.Private {
			continue // its index is skipped, so the pair around it isn't consecutive
		}
		idx = append(idx, int32(i))
		lons = append(lons, p.Lon)
		lats = append(lats, p.Lat)
		ts = append(ts, float64(p.Time.UnixMilli())/1000)
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM spot_visits WHERE activity_id = $1`, activityID); err != nil {
			return fmt.Errorf("spots: clear visits: %w", err)
		}
		if len(idx) < 2 {
			return nil
		}
		if _, err := tx.Exec(ctx, matchSQL, userID, activityID, idx, lons, lats, ts, MinDwellSeconds); err != nil {
			return fmt.Errorf("spots: match: %w", err)
		}
		return nil
	})
}

// matchSQL: `candidates` is the indexed trajectory intersection, `inside` every point in each
// candidate, and `steps` the gap back to the previous point inside the same spot — counted
// only when that previous point is the one right before it (di = 1), so leaving a spot and
// coming back later doesn't count the time spent away.
const matchSQL = `
WITH candidates AS MATERIALIZED (
    SELECT s.id, s.geom FROM spots s
    WHERE ST_Intersects(s.geom, (SELECT trajectory FROM activities WHERE id = $2))
),
pts AS (
    SELECT i, t, ST_SetSRID(ST_MakePoint(lon, lat), 4326) AS g
    FROM unnest($3::int[], $4::float8[], $5::float8[], $6::float8[]) AS p(i, lon, lat, t)
),
inside AS (
    SELECT c.id AS spot_id, p.i, p.t
    FROM candidates c JOIN pts p ON ST_Intersects(c.geom, p.g)
),
steps AS (
    SELECT spot_id, t - lag(t) OVER w AS dt, i - lag(i) OVER w AS di, lag(t) OVER w AS prev_t
    FROM inside
    WINDOW w AS (PARTITION BY spot_id ORDER BY i)
)
INSERT INTO spot_visits (user_id, spot_id, activity_id, visited_at, dwell_seconds)
SELECT $1, spot_id, $2, to_timestamp(min(prev_t)), round(sum(dt))::int
FROM steps
WHERE di = 1
GROUP BY spot_id
HAVING sum(dt) >= $7`
