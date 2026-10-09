package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Overlapping activities — IMPLEMENTATION.md §4.6.
//
// Nothing is matched or hidden on ingest (ADR-0039): the same ride from two sources is two
// activities if the user ticks both. What's here is the hint that says so before they do,
// and the one place activities hidden as duplicates before that are still ranked.
//
// Two activities overlap when their time ranges overlap by at least overlapMinShare of the
// *longer* one's duration. One person is not on two rides at once, so the stretch of time an
// activity covers is the one thing every copy of it agrees on. Type and distance are not
// compared: sources disagree on both for the same ride. Measuring against the longer duration
// keeps back-to-back recordings with a few seconds of clock skew, a ten-minute walk inside a
// two-hour hike and a day hike inside a multi-day log apart.
const overlapMinShare = 0.8

// overlapStartSlack bounds how far apart two overlapping starts can be, as a fraction of the
// candidate's duration D, so the lookup is a plain range scan on `(user_id, started_at)`. An
// overlap needs ≥ f·max, and overlap ≤ max − |Δstart|, so |Δstart| ≤ (1−f)·max; and since
// min ≥ f·max, max ≤ D/f. Together: |Δstart| ≤ (1−f)/f · D.
const overlapStartSlack = (1 - overlapMinShare) / overlapMinShare

// overlapMatches is the rule on its own, for tests; Overlaps' SQL mirrors it.
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
	return hi.Sub(lo).Seconds() >= overlapMinShare*max(aDur, bDur).Seconds()
}

// Span is one candidate's time range, named by the caller's own key.
type Span struct {
	Key   string
	Start time.Time
	End   time.Time
}

// Overlap is the live activity a Span overlaps the most.
type Overlap struct {
	Key          string
	ActivityID   string
	Name         string
	ActivityType string
	StartedAt    time.Time
	Timezone     *string
}

// Overlaps answers, for each span, the account's live activity it overlaps the most, if any.
// A span with no duration overlaps nothing.
func Overlaps(ctx context.Context, pool *pgxpool.Pool, userID string, spans []Span) ([]Overlap, error) {
	keys := make([]string, 0, len(spans))
	starts := make([]time.Time, 0, len(spans))
	ends := make([]time.Time, 0, len(spans))
	for _, s := range spans {
		if !s.End.After(s.Start) {
			continue
		}
		keys = append(keys, s.Key)
		starts = append(starts, s.Start)
		ends = append(ends, s.End)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		WITH span AS (
			SELECT key, s, e, extract(epoch FROM e - s)::float8 AS secs
			FROM unnest($2::text[], $3::timestamptz[], $4::timestamptz[]) AS t(key, s, e)
		)
		SELECT DISTINCT ON (span.key) span.key, a.id::text, COALESCE(a.name, ''), a.activity_type, a.started_at, a.timezone
		FROM span
		JOIN activities a ON a.user_id = $1
		  AND a.superseded_by IS NULL
		  AND a.duration_seconds > 0
		  AND a.started_at BETWEEN span.s - make_interval(secs => span.secs * $5::float8)
		                       AND span.s + make_interval(secs => span.secs * $5::float8)
		CROSS JOIN LATERAL (
			SELECT extract(epoch FROM
			         least(a.started_at + make_interval(secs => a.duration_seconds), span.e)
			         - greatest(a.started_at, span.s))::float8 AS secs
		) shared
		-- overlapMatches: the overlap covers overlapMinShare of the longer of the two.
		WHERE shared.secs >= $6::float8 * greatest(a.duration_seconds::float8, span.secs)
		ORDER BY span.key, shared.secs DESC, a.started_at, a.id
	`, userID, keys, starts, ends, overlapStartSlack, overlapMinShare)
	if err != nil {
		return nil, fmt.Errorf("ingest: overlaps: %w", err)
	}
	defer rows.Close()
	var out []Overlap
	for rows.Next() {
		var o Overlap
		if err := rows.Scan(&o.Key, &o.ActivityID, &o.Name, &o.ActivityType, &o.StartedAt, &o.Timezone); err != nil {
			return nil, fmt.Errorf("ingest: scan overlap: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// candidate is one released copy, with everything needed to rank it.
type candidate struct {
	id             string
	hasTrajectory  bool
	streamChannels int
	pointCount     int
	createdAt      time.Time
}

// richerThan is "prefer the richest record": geometry over none, then more stream channels
// over fewer.
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

// RankReleased keeps one of the copies a deleted activity hid as duplicates, before it's
// deleted: `superseded_by` is `ON DELETE SET NULL`, so every copy it displaced would become
// live at once and the same ride would count two or three times. The richest is promoted and
// the rest point at it instead. Only copies hidden before ingest stopped matching (ADR-0039)
// can be here, and nothing new is ever matched: the rank is among the released copies alone.
// Reports the z14 tiles the promotion changes.
func RankReleased(ctx context.Context, pool *pgxpool.Pool, deletedID string) ([][2]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id::text,
		       a.trajectory IS NOT NULL,
		       -- Not a plain IS NOT NULL on the column: it's an array with one element per point,
		       -- so a track with no elevation still has a non-null array full of nulls. The
		       -- channel counts only if some element holds a reading. Elevation is the only
		       -- stream channel: HoldMyTrack keeps no heart rate (VISION.md §1.1).
		       (EXISTS (SELECT 1 FROM unnest(s.elevation_m) v WHERE v IS NOT NULL))::int,
		       COALESCE(s.point_count, 0),
		       a.created_at
		FROM activities a
		LEFT JOIN activity_streams s ON s.activity_id = a.id
		WHERE a.superseded_by = $1
	`, deletedID)
	if err != nil {
		return nil, fmt.Errorf("ingest: released copies: %w", err)
	}
	defer rows.Close()
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.hasTrajectory, &c.streamChannels, &c.pointCount, &c.createdAt); err != nil {
			return nil, fmt.Errorf("ingest: scan released copy: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: read released copies: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	winner := candidates[0]
	for _, c := range candidates[1:] {
		if c.richerThan(winner) {
			winner = c
		}
	}
	tiles, err := tilesForActivities(ctx, pool, []string{winner.id})
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE activities SET superseded_by = CASE WHEN id = $2 THEN NULL ELSE $2::uuid END
		WHERE superseded_by = $1
	`, deletedID, winner.id); err != nil {
		return nil, fmt.Errorf("ingest: promote released copy: %w", err)
	}
	return tiles, nil
}

// tilesForActivities returns every z14 tile the given activities rendered a mask for — the
// exact set whose aggregate has to be recomposited once one of them is promoted.
func tilesForActivities(ctx context.Context, pool *pgxpool.Pool, activityIDs []string) ([][2]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT tile_x, tile_y FROM activity_tile_masks
		WHERE activity_id = ANY($1::uuid[]) AND zoom = $2
	`, activityIDs, FogZoom)
	if err != nil {
		return nil, fmt.Errorf("ingest: released copy tiles: %w", err)
	}
	defer rows.Close()
	var tiles [][2]int
	for rows.Next() {
		var x, y int
		if err := rows.Scan(&x, &y); err != nil {
			return nil, fmt.Errorf("ingest: scan released copy tile: %w", err)
		}
		tiles = append(tiles, [2]int{x, y})
	}
	return tiles, rows.Err()
}
