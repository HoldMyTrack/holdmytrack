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
// ride arriving by three paths genuinely do have three different ids. What they share is the
// stretch of time they were recorded over — one person is not on two rides at once — so
// identity is time overlap: two activities are the same when their time ranges overlap by at
// least dedupeMinOverlap of the *longer* one's duration.
//
// Type and distance are deliberately not compared. Sources disagree on both for the same ride —
// "walking" against "hiking", or a type-less "unknown"; distance measured by a watch, a phone,
// a smoothed export or a wheel sensor, a few percent apart — and every such disagreement was a
// missed duplicate. Start times drift too (a late GPS lock, trimmed auto-pause), which an
// overlap fraction absorbs without a separate tolerance.
//
// Measuring against the longer duration, not the shorter, is what keeps the rule strict in the
// cases where an overlap is not the same activity: a few seconds of clock skew between two
// back-to-back recordings, an auto-detected ten-minute walk inside a two-hour hike, a day hike
// inside a multi-day log. All of those stay separate. A duplicate that survives is visible and
// fixable; a real activity merged away is not.
const dedupeMinOverlap = 0.8

// dedupeStartSlack bounds how far apart two matching starts can be, as a fraction of the
// incoming activity's duration D, so the candidate lookup is a plain range scan on
// `(user_id, started_at)`. A match needs overlap ≥ f·max, and overlap ≤ max − |Δstart|, so
// |Δstart| ≤ (1−f)·max; and since min ≥ f·max, max ≤ D/f. Together: |Δstart| ≤ (1−f)/f · D.
const dedupeStartSlack = (1 - dedupeMinOverlap) / dedupeMinOverlap

// overlapMatches is the match rule on its own, for tests; ResolveDuplicates' SQL mirrors it.
func overlapMatches(aStart time.Time, aDur time.Duration, bStart time.Time, bDur time.Duration) bool {
	if aDur <= 0 || bDur <= 0 {
		return false
	}
	lo, hi := aStart, aStart.Add(aDur)
	if bStart.After(lo) {
		lo = bStart
	}
	if bEnd := bStart.Add(bDur); bEnd.Before(hi) {
		hi = bEnd
	}
	return hi.Sub(lo).Seconds() >= dedupeMinOverlap*max(aDur, bDur).Seconds()
}

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
	userID string,
	startedAt time.Time,
	durationS int,
) ([][2]int, error) {
	// No time span, nothing to overlap — never deduplicated.
	if durationS <= 0 {
		return nil, nil
	}
	duration := time.Duration(durationS) * time.Second
	slack := time.Duration(float64(duration) * dedupeStartSlack)
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
		  AND a.started_at BETWEEN $2 AND $3
		  AND a.duration_seconds > 0
		  -- overlapMatches: the overlap covers dedupeMinOverlap of the longer of the two.
		  AND extract(epoch FROM
		        least(a.started_at + make_interval(secs => a.duration_seconds), $5::timestamptz)
		        - greatest(a.started_at, $4::timestamptz))::float8
		      >= $6::float8 * greatest(a.duration_seconds, $7::int)
	`,
		userID,
		startedAt.Add(-slack), startedAt.Add(slack),
		startedAt, startedAt.Add(duration),
		dedupeMinOverlap, durationS,
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
