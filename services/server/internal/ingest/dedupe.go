package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cross-source deduplication — IMPLEMENTATION.md §4.6.
//
// The unique index on `(user_id, source, external_id)` cannot help here: three copies of one
// ride arriving by three paths genuinely do have three different ids. What they share is
// roughly when they started and roughly how far they went, so identity has to be fuzzy.
//
// §4.6's tolerance is **start time within a minute, distance within ~1%**, and it is applied
// here as a window around the incoming activity rather than as equality on a pre-rounded
// bucket. Rounding first is cheaper to index but turns matching into a lottery at the bucket
// edges — two copies three seconds apart match or don't depending purely on whether they
// straddle a boundary, which was measured happening on a real pair. A window applies the same
// tolerance the spec names, to every pair, the same way.
//
// The bias §4.6 asks for still holds: an activity with no distance is never deduplicated at
// all, because type and start minute alone are too weak to merge on, and a duplicate that
// survives is visible and fixable in a way a merge of two real activities is not.
const (
	// dedupeTimeWindow is §4.6's "nearest minute" as a symmetric range: two starts within
	// half a minute either way are the same start.
	dedupeTimeWindow = 30 * time.Second

	// dedupeDistanceTolerance is §4.6's ~1%, relative rather than absolute — 1% of a 5 km run
	// and 1% of a 200 km ride are very different numbers of metres, and a fixed-metre
	// tolerance would be far too strict for one and far too loose for the other.
	dedupeDistanceTolerance = 0.01
)

// candidate is one row in a collision, with everything needed to rank it.
type candidate struct {
	id             string
	hasTrajectory  bool
	streamChannels int
	pointCount     int
	createdAt      time.Time
}

// richerThan implements §4.6's "prefer the richest record": geometry over none, then more
// stream channels over fewer.
//
// The last two comparisons are not richness judgements, they are a tie-break that has to be
// total and stable: two identical-looking copies must resolve the same way no matter which
// one was examined first, or a re-run could flip which one is live and churn every fog tile
// underneath it. Oldest wins, since that is the copy anything already holding an activity id
// is most likely holding.
func (c candidate) richerThan(other candidate) bool {
	if c.hasTrajectory != other.hasTrajectory {
		return c.hasTrajectory
	}
	if c.streamChannels != other.streamChannels {
		return c.streamChannels > other.streamChannels
	}
	if c.pointCount != other.pointCount {
		return c.pointCount > other.pointCount
	}
	if !c.createdAt.Equal(other.createdAt) {
		return c.createdAt.Before(other.createdAt)
	}
	return c.id < other.id
}

// ResolveDuplicates settles the collision around one activity and reports the z14 tiles whose
// fog aggregate the outcome invalidated.
//
// Exported because ingest is not the only thing that disturbs a collision: deleting the copy
// that won releases every copy it displaced at once (`superseded_by` is `ON DELETE SET NULL`),
// and without re-ranking them the ride would be live two or three times over — the exact
// double-counting §4.6 exists to prevent, and visible immediately in the additive heatmap.
//
// Runs after the activity is fully persisted — streams and masks included — because richness
// is read off those rows: ranking a record before its own streams exist would judge it the
// poorest copy every time.
//
// The whole window is re-ranked, superseded rows included, not just "new versus incumbent". A
// third copy arriving richer than both has to take over from whichever was live, and
// re-ranking is the only way that stays correct without tracking how the previous decision
// was reached.
func ResolveDuplicates(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, activityType string,
	startedAt time.Time,
	distanceM float64,
) ([][2]int, error) {
	if distanceM <= 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT a.id::text,
		       a.trajectory IS NOT NULL,
		       -- Not a plain IS NOT NULL on the column: these are arrays, and ingest always
		       -- writes one element per point, so a track that carried no elevation at all
		       -- still has a non-null array full of nulls. Measured the hard way — every
		       -- activity scored two channels and the richness comparison never decided
		       -- anything. A channel counts only if some element actually holds a reading,
		       -- and EXISTS stops at the first one it finds.
		       (EXISTS (SELECT 1 FROM unnest(s.elevation_m) v WHERE v IS NOT NULL))::int
		         + (EXISTS (SELECT 1 FROM unnest(s.heartrate) v WHERE v IS NOT NULL))::int,
		       COALESCE(s.point_count, 0),
		       a.created_at
		FROM activities a
		LEFT JOIN activity_streams s ON s.activity_id = a.id
		WHERE a.user_id = $1
		  AND a.activity_type = $2
		  AND a.started_at BETWEEN $3 AND $4
		  AND a.distance_meters IS NOT NULL
		  AND abs(a.distance_meters - $5::numeric) <= $6::numeric
	`,
		userID, activityType,
		startedAt.Add(-dedupeTimeWindow), startedAt.Add(dedupeTimeWindow),
		distanceM, distanceM*dedupeDistanceTolerance,
	)
	if err != nil {
		return nil, fmt.Errorf("ingest: dedupe candidates: %w", err)
	}
	defer rows.Close()

	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.hasTrajectory, &c.streamChannels, &c.pointCount, &c.createdAt); err != nil {
			return nil, fmt.Errorf("ingest: scan dedupe candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: read dedupe candidates: %w", err)
	}
	if len(candidates) < 2 {
		return nil, nil
	}

	winner := candidates[0]
	for _, c := range candidates[1:] {
		if c.richerThan(winner) {
			winner = c
		}
	}
	losers := make([]string, 0, len(candidates)-1)
	for _, c := range candidates {
		if c.id != winner.id {
			losers = append(losers, c.id)
		}
	}

	// Read the affected tiles before writing, and for every row in the window rather than
	// just the losers'. A copy being *promoted* here was contributing nothing to the
	// aggregate a moment ago, and its tiles have to be rebuilt too.
	ids := append(append([]string{}, losers...), winner.id)
	tiles, err := tilesForActivities(ctx, pool, ids)
	if err != nil {
		return nil, err
	}

	if _, err := pool.Exec(ctx, `
		UPDATE activities SET superseded_by = $1 WHERE id = ANY($2::uuid[])
	`, winner.id, losers); err != nil {
		return nil, fmt.Errorf("ingest: mark superseded: %w", err)
	}
	// The winner may itself have been superseded by an earlier, poorer decision — a richer
	// copy arriving third has to be able to take the place back.
	if _, err := pool.Exec(ctx, `
		UPDATE activities SET superseded_by = NULL WHERE id = $1
	`, winner.id); err != nil {
		return nil, fmt.Errorf("ingest: clear superseded on winner: %w", err)
	}
	return tiles, nil
}

// tilesForActivities returns every z14 tile the given activities rendered a mask for — the
// exact set whose aggregate has to be recomposited once membership changes.
func tilesForActivities(ctx context.Context, pool *pgxpool.Pool, activityIDs []string) ([][2]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT tile_x, tile_y FROM activity_tile_masks
		WHERE activity_id = ANY($1::uuid[]) AND zoom = $2
	`, activityIDs, FogZoom)
	if err != nil {
		return nil, fmt.Errorf("ingest: dedupe touched tiles: %w", err)
	}
	defer rows.Close()
	var tiles [][2]int
	for rows.Next() {
		var x, y int
		if err := rows.Scan(&x, &y); err != nil {
			return nil, fmt.Errorf("ingest: scan dedupe touched tile: %w", err)
		}
		tiles = append(tiles, [2]int{x, y})
	}
	return tiles, rows.Err()
}
