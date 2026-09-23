package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/fog"
	"github.com/fitmap/fitmap/services/server/internal/geo"
	"github.com/fitmap/fitmap/services/server/internal/parse"
	"github.com/fitmap/fitmap/services/server/internal/storage"
)

// TrackEdit is a user's edit to an activity's recorded points (IMPLEMENTATION.md §4.7.7),
// stored in activities.track_edit and replayed on top of the parsed, privacy-trimmed points
// every time the activity is processed. Every value is a point timestamp in unix
// milliseconds, never a point index: indices shift whenever the privacy trim changes,
// timestamps don't.
//
// A point survives when it lies inside Keep (inclusive; nil keeps everything), outside every
// Remove range (inclusive), and its timestamp isn't in Drop. Chop narrows Keep, Cut adds a
// Remove range spanning the points strictly between its two ends (so the ends themselves
// survive and are joined), and Delete point adds to Drop. Points sharing one timestamp are
// kept or removed together — see EditableTimestamps for the one ordering the spec relies on.
type TrackEdit struct {
	Keep   *[2]int64  `json:"keep,omitempty"`
	Remove [][2]int64 `json:"remove,omitempty"`
	Drop   []int64    `json:"drop,omitempty"`
}

// IsEmpty reports whether the edit changes nothing — stored as NULL, not as an empty object.
func (e TrackEdit) IsEmpty() bool {
	return e.Keep == nil && len(e.Remove) == 0 && len(e.Drop) == 0
}

// Validate rejects a malformed spec — a range whose start is after its end. It says nothing
// about whether enough points survive; only Apply against the real points can tell that.
func (e TrackEdit) Validate() error {
	if e.Keep != nil && e.Keep[0] > e.Keep[1] {
		return errors.New("keep range starts after it ends")
	}
	for _, r := range e.Remove {
		if r[0] > r[1] {
			return errors.New("remove range starts after it ends")
		}
	}
	return nil
}

// Apply returns the points that survive the edit, in their original order.
func (e TrackEdit) Apply(points []parse.Point) []parse.Point {
	if e.IsEmpty() {
		return points
	}
	drop := make(map[int64]struct{}, len(e.Drop))
	for _, t := range e.Drop {
		drop[t] = struct{}{}
	}
	out := make([]parse.Point, 0, len(points))
	for _, p := range points {
		t := p.Time.UnixMilli()
		if e.Keep != nil && (t < e.Keep[0] || t > e.Keep[1]) {
			continue
		}
		if _, ok := drop[t]; ok {
			continue
		}
		removed := false
		for _, r := range e.Remove {
			if t >= r[0] && t <= r[1] {
				removed = true
				break
			}
		}
		if !removed {
			out = append(out, p)
		}
	}
	return out
}

// ErrNotEditable is why a track can't be edited at all: the spec addresses points by
// timestamp, which only works when timestamps run forward. A planned route with no times, or
// a file whose clock jumps backwards, has no way to say "the points from here to there."
var ErrNotEditable = errors.New("this track's points have no usable timestamps, so it can't be edited")

// EditableTimestamps checks the one property TrackEdit relies on: timestamps never decrease
// and the track spans some time at all. Repeated timestamps are tolerated — those points are
// simply edited together.
func EditableTimestamps(points []parse.Point) error {
	if len(points) < 2 || !points[len(points)-1].Time.After(points[0].Time) {
		return ErrNotEditable
	}
	for i := 1; i < len(points); i++ {
		if points[i].Time.Before(points[i-1].Time) {
			return ErrNotEditable
		}
	}
	return nil
}

// EditJob is the `edit_track` job's payload. Edit is the complete new spec, not a delta —
// nil means "reset to the original track".
type EditJob struct {
	UserID     string     `json:"user_id"`
	ActivityID string     `json:"activity_id"`
	Edit       *TrackEdit `json:"edit"`
}

// ProcessTrackEdit reprocesses one activity with a new edit spec: the same parse → trim →
// metrics → simplify → masks pipeline as Process, ending in an UPDATE of the existing row
// instead of an INSERT. On any failure the activity's edit_pending flag is cleared again, so a
// failed edit leaves the activity as it was rather than stuck showing Pending.
func ProcessTrackEdit(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job EditJob) error {
	err := processTrackEdit(ctx, pool, store, job)
	if err != nil {
		if _, uerr := pool.Exec(ctx,
			`UPDATE activities SET edit_pending = false WHERE id = $1 AND user_id = $2`,
			job.ActivityID, job.UserID,
		); uerr != nil {
			return fmt.Errorf("%w (and clearing edit_pending failed: %v)", err, uerr)
		}
	}
	return err
}

func processTrackEdit(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job EditJob) error {
	var sourceDetail string
	var rawKey *string
	var trimCm int
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(a.source_detail, ''), a.raw_payload_key, u.privacy_trim_cm
		FROM activities a JOIN users u ON u.id = a.user_id
		WHERE a.id = $1 AND a.user_id = $2
	`, job.ActivityID, job.UserID).Scan(&sourceDetail, &rawKey, &trimCm)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deleted while the job was queued: nothing left to edit
	}
	if err != nil {
		return fmt.Errorf("edit: load activity: %w", err)
	}
	if rawKey == nil {
		return errors.New("edit: activity has no raw payload to reprocess")
	}

	// The account's current Privacy Trim, the same value the editor's own point list was
	// trimmed with (handleActivityTrackPoints) — so what the user saw is what gets processed.
	points, err := LoadTrimmedPoints(ctx, store, sourceDetail, *rawKey, float64(trimCm)/100)
	if err != nil {
		return err
	}
	var editJSON []byte
	if job.Edit != nil && !job.Edit.IsEmpty() {
		points = job.Edit.Apply(points)
		if editJSON, err = json.Marshal(job.Edit); err != nil {
			return err
		}
	}
	if len(points) < 2 {
		return fmt.Errorf("edit: fewer than 2 points survive the edit (%d)", len(points))
	}

	pp, err := prepareTrack(ctx, pool, points)
	if err != nil {
		return err
	}
	m := pp.m

	oldTiles, err := ActivityTiles(ctx, pool, job.ActivityID)
	if err != nil {
		return fmt.Errorf("edit: read old tiles: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	// started_at moves with a Chop — the activity now starts where its first kept point does.
	// in_heatmap_window follows it, the same predicate migrations/0018 backfilled with.
	if _, err := tx.Exec(ctx, `
		UPDATE activities SET
			distance_meters = $3, duration_seconds = $4, moving_seconds = $5,
			elevation_gain_m = $6, avg_speed_mps = $7, started_at = $8,
			trajectory = ST_SetSRID(
				ST_MakeLine(ARRAY(
					SELECT ST_MakePointM(lon, lat, t)
					FROM unnest($9::float8[], $10::float8[], $11::float8[]) AS pt(lon, lat, t)
				)),
				4326
			),
			track_edit = $12::jsonb,
			in_heatmap_window = ($8 >= NOW() - make_interval(days => $13))
		WHERE id = $1 AND user_id = $2
	`, job.ActivityID, job.UserID,
		m.distanceM, m.durationS, m.movingS, m.elevationGainM, m.avgSpeedMps, points[0].Time,
		pp.simpLons, pp.simpLats, pp.simpTs,
		editJSON, fog.HeatmapWindowDays,
	); err != nil {
		return fmt.Errorf("edit: update activity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO activity_streams (activity_id, point_count, elapsed_s, elevation_m, heartrate, dist_m)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (activity_id) DO UPDATE SET
			point_count = EXCLUDED.point_count, elapsed_s = EXCLUDED.elapsed_s,
			elevation_m = EXCLUDED.elevation_m, heartrate = EXCLUDED.heartrate, dist_m = EXCLUDED.dist_m
	`, job.ActivityID, len(points), pp.elapsedS, pp.elevM, pp.hr, pp.distM); err != nil {
		return fmt.Errorf("edit: update streams: %w", err)
	}
	// Country/Region matches are insert-only (geo.MatchActivity), so an edit that removed the
	// only part of a track inside some region has to clear the old matches first.
	if _, err := tx.Exec(ctx, `DELETE FROM activity_country WHERE activity_id = $1`, job.ActivityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM activity_region WHERE activity_id = $1`, job.ActivityID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Masks: re-render every tile the edited track touches (overwriting in place), then drop
	// the ones it no longer touches. Overwriting rather than clearing first means a tile the
	// track still crosses never goes through a moment with no mask for this activity.
	newTiles := computeTouchedTiles(points, FogZoom)
	if err := fog.RenderActivityMasks(ctx, pool, store, job.ActivityID, points, newTiles); err != nil {
		return fmt.Errorf("edit: render activity masks: %w", err)
	}
	if err := fog.RemoveActivityMasks(ctx, pool, store, job.ActivityID, tilesNotIn(oldTiles, newTiles)); err != nil {
		return fmt.Errorf("edit: remove stale masks: %w", err)
	}

	if err := geo.MatchActivity(ctx, pool, job.ActivityID); err != nil {
		return fmt.Errorf("edit: match admin boundaries: %w", err)
	}

	// Old ∪ new: a tile the edit cut the track out of needs recompositing just as much as one
	// it still crosses. Deduplication is deliberately not re-run (§4.7.7) — the edit changes
	// this copy's distance, not which copy of the ride is the real one.
	if err := MarkFogTilesDirty(ctx, pool, job.UserID, mergeTiles(oldTiles, newTiles)); err != nil {
		return fmt.Errorf("edit: mark fog tiles dirty: %w", err)
	}
	// Rendered here rather than enqueued as a `render_fog` job the way ingest does it: Pending
	// promises the client that once it clears, *everything* derived from the points — Fog and
	// Heatmap included — is current, and the client refetches both rasters at exactly that
	// moment. A separate job would clear Pending seconds before the rasters caught up, and the
	// refetch would pick up the old tiles. RenderUser only renders dirty tiles, so this costs
	// the same as the job would have.
	if err := fog.RenderUser(ctx, pool, store, job.UserID); err != nil {
		return fmt.Errorf("edit: render fog/heatmap: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE activities SET edit_pending = false WHERE id = $1 AND user_id = $2`,
		job.ActivityID, job.UserID,
	); err != nil {
		return fmt.Errorf("edit: clear edit_pending: %w", err)
	}
	return nil
}

// tilesNotIn returns the tiles of a that are absent from b.
func tilesNotIn(a, b [][2]int) [][2]int {
	in := make(map[[2]int]struct{}, len(b))
	for _, t := range b {
		in[t] = struct{}{}
	}
	var out [][2]int
	for _, t := range a {
		if _, ok := in[t]; !ok {
			out = append(out, t)
		}
	}
	return out
}

// EnqueueTrackEdit marks the activity pending and enqueues its `edit_track` job in one
// transaction, so an activity is never shown Pending without a job on its way, nor gets a job
// while already pending. Returns false when the activity isn't this user's, is a superseded
// duplicate, or already has an edit pending.
func EnqueueTrackEdit(ctx context.Context, pool *pgxpool.Pool, job EditJob) (bool, error) {
	payload, err := json.Marshal(job)
	if err != nil {
		return false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	tag, err := tx.Exec(ctx, `
		UPDATE activities SET edit_pending = true
		WHERE id = $1 AND user_id = $2 AND superseded_by IS NULL AND NOT edit_pending
	`, job.ActivityID, job.UserID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (kind, user_id, payload) VALUES ('edit_track', $1, $2)`,
		job.UserID, payload,
	); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
