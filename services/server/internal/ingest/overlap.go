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
// activities if the user ticks both. What's here is the hint that says so before they do.
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

// Overlaps answers, for each span, the account's live activity it overlaps the most, if any,
// each activity taken as recorded (recordedSpan). A span with no duration overlaps nothing.
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
		  AND COALESCE(a.recorded_started_at, a.started_at)
		      BETWEEN span.s - make_interval(secs => span.secs * $5::float8)
		          AND span.s + make_interval(secs => span.secs * $5::float8)
		-- As recorded, before Private locations and track edits; a row stored before those
		-- columns existed falls back to its clipped track.
		CROSS JOIN LATERAL (
			SELECT COALESCE(a.recorded_started_at, a.started_at) AS s,
			       COALESCE(a.recorded_ended_at, a.started_at + make_interval(secs => a.duration_seconds)) AS e
		) rec
		CROSS JOIN LATERAL (
			SELECT extract(epoch FROM least(rec.e, span.e) - greatest(rec.s, span.s))::float8 AS secs,
			       extract(epoch FROM rec.e - rec.s)::float8 AS dur
		) shared
		-- overlapMatches: the overlap covers overlapMinShare of the longer of the two.
		WHERE shared.dur > 0
		  AND shared.secs >= $6::float8 * greatest(shared.dur, span.secs)
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
