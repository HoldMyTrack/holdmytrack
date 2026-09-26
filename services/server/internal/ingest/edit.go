package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/geo"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// TrackEdit is a user's edit to an activity's recorded points (IMPLEMENTATION.md §4.7.7),
// stored in activities.track_edit and replayed on top of the parsed, privacy-clipped points
// every time the activity is processed. Every value is a point timestamp in unix
// milliseconds, never a point index: indices shift whenever a Private location changes,
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

// ProcessTrackEdit reprocesses one activity with a new edit spec: the same parse → clip →
// metrics → simplify → masks pipeline as Process, ending in an UPDATE of the existing row
// instead of an INSERT. On any failure the activity's edit_pending flag is cleared again and
// its tiles re-rendered, so a failed edit leaves the activity as it was — back in Fog/Heatmap
// with its old masks — rather than stuck showing Pending.
func ProcessTrackEdit(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job EditJob) error {
	err := processTrackEdit(ctx, pool, store, job)
	if err != nil {
		if _, uerr := pool.Exec(ctx,
			`UPDATE activities SET edit_pending = false WHERE id = $1 AND user_id = $2`,
			job.ActivityID, job.UserID,
		); uerr != nil {
			return fmt.Errorf("%w (and clearing edit_pending failed: %v)", err, uerr)
		}
		if rerr := fog.RenderUser(ctx, pool, store, job.UserID); rerr != nil {
			return fmt.Errorf("%w (and re-rendering fog/heatmap failed: %v)", err, rerr)
		}
	}
	return err
}

func processTrackEdit(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job EditJob) error {
	edit := job.Edit
	if edit == nil {
		edit = &TrackEdit{} // reset: an explicitly empty spec, not "keep the stored one"
	}
	// Rendered here, in the job, rather than enqueued as `render_fog` jobs the way ingest does
	// it — a separate job could run alongside this one and race it over the same dirty flags.
	// First, with the activity Pending (EnqueueTrackEdit dirtied its tiles), so it drops out
	// of Fog/Heatmap while it's reprocessed.
	if err := fog.RenderUser(ctx, pool, store, job.UserID); err != nil {
		return fmt.Errorf("edit: render fog/heatmap: %w", err)
	}
	if err := reprocessActivity(ctx, pool, store, job.UserID, job.ActivityID, edit); err != nil {
		return fmt.Errorf("edit: %w", err)
	}
	// A Private location change queued while this edit ran reprocesses the activity again;
	// its badge stays on until that lands.
	if _, err := pool.Exec(ctx, `
		UPDATE activities a SET edit_pending = false
		WHERE a.id = $1 AND a.user_id = $2
		  AND NOT EXISTS (
			SELECT 1 FROM jobs j
			WHERE j.user_id = $2 AND j.kind = 'reprivacy' AND j.state = 'pending'
			  AND j.payload->'activity_ids' ? a.id::text
		  )
	`, job.ActivityID, job.UserID); err != nil {
		return fmt.Errorf("edit: clear edit_pending: %w", err)
	}
	// Then again once Pending is cleared, which brings it back with its new masks. The badge
	// clears a moment before these tiles land, so the client refreshes Fog/Heatmap off the
	// coverage status — this job counts as rendering until it returns — not off the badge.
	// RenderUser only renders dirty tiles, so each pass costs what one `render_fog` would.
	if err := fog.RenderUser(ctx, pool, store, job.UserID); err != nil {
		return fmt.Errorf("edit: render fog/heatmap: %w", err)
	}
	return nil
}

// errFewPointsAfterEdit is a user's edit leaving less than a line — rejected, unlike a Private
// location hiding the whole track, which is a valid outcome (the activity just has no geometry).
var errFewPointsAfterEdit = errors.New("fewer than 2 points survive the edit")

// reprocessActivity re-derives one activity from its raw payload: parse, clip against the
// account's current Private locations, apply a track edit, then update metrics, trajectory,
// streams, masks and regions, and mark every tile it touched before or after dirty. It neither
// renders those tiles nor clears edit_pending — its callers decide when (§4.7.7's edit_track
// does both straight away; a `reprivacy` job once for the whole batch).
//
// edit == nil replays the activity's stored track_edit unchanged; otherwise edit replaces it
// (empty meaning reset). An activity deleted meanwhile is skipped without error.
func reprocessActivity(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, activityID string, edit *TrackEdit) error {
	var sourceDetail string
	var rawKey *string
	var storedEdit []byte
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(source_detail, ''), raw_payload_key, track_edit
		FROM activities WHERE id = $1 AND user_id = $2
	`, activityID, userID).Scan(&sourceDetail, &rawKey, &storedEdit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deleted while the job was queued: nothing left to reprocess
	}
	if err != nil {
		return fmt.Errorf("load activity: %w", err)
	}
	if rawKey == nil {
		return errors.New("activity has no raw payload to reprocess")
	}

	userEdit := edit != nil
	if !userEdit && storedEdit != nil {
		edit = &TrackEdit{}
		if err := json.Unmarshal(storedEdit, edit); err != nil {
			return fmt.Errorf("stored track edit unreadable: %w", err)
		}
	}

	// Clipped with the account's current Private locations — the same points the editor's own
	// list was built from (handleActivityTrackPoints), so what the user saw is what's processed.
	act, points, err := loadClippedPoints(ctx, pool, store, userID, sourceDetail, *rawKey)
	if err != nil {
		return err
	}
	var editJSON []byte
	if edit != nil && !edit.IsEmpty() {
		if points != nil {
			points = edit.Apply(points)
		}
		if editJSON, err = json.Marshal(edit); err != nil {
			return err
		}
	}
	if len(points) < 2 {
		if userEdit && points != nil {
			return errFewPointsAfterEdit
		}
		points = nil // hidden by Private locations, or by a stored edit on top of them
	}

	startedAt := act.Points[0].Time
	var pp preparedTrack
	if points != nil {
		if pp, err = prepareTrack(ctx, pool, points); err != nil {
			return err
		}
		startedAt = points[0].Time
	}
	m := pp.m

	oldTiles, err := ActivityTiles(ctx, pool, activityID)
	if err != nil {
		return fmt.Errorf("read old tiles: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	// started_at moves with a Chop — the activity now starts where its first kept point does.
	// in_heatmap_window follows it, the same predicate migrations/0018 backfilled with.
	// track_edit is only rewritten by a user's edit; a reprivacy pass leaves it as stored.
	if _, err := tx.Exec(ctx, `
		UPDATE activities SET
			distance_meters = $3, duration_seconds = $4, moving_seconds = $5,
			elevation_gain_m = $6, avg_speed_mps = $7, started_at = $8,
			trajectory = `+trajectorySQL("$9", "$10", "$11")+`,
			track_edit = CASE WHEN $14 THEN $12::jsonb ELSE track_edit END,
			in_heatmap_window = ($8 >= NOW() - make_interval(days => $13))
		WHERE id = $1 AND user_id = $2
	`, activityID, userID,
		m.distanceM, m.durationS, m.movingS, m.elevationGainM, m.avgSpeedMps, startedAt,
		pp.simpLons, pp.simpLats, pp.simpTs,
		editJSON, fog.HeatmapWindowDays, userEdit,
	); err != nil {
		return fmt.Errorf("update activity: %w", err)
	}
	if points == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM activity_streams WHERE activity_id = $1`, activityID); err != nil {
			return fmt.Errorf("delete streams: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `
		INSERT INTO activity_streams (activity_id, point_count, elapsed_s, elevation_m, heartrate, dist_m)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (activity_id) DO UPDATE SET
			point_count = EXCLUDED.point_count, elapsed_s = EXCLUDED.elapsed_s,
			elevation_m = EXCLUDED.elevation_m, heartrate = EXCLUDED.heartrate, dist_m = EXCLUDED.dist_m
	`, activityID, len(points), pp.elapsedS, pp.elevM, pp.hr, pp.distM); err != nil {
		return fmt.Errorf("update streams: %w", err)
	}
	// Country/Region matches are insert-only (geo.MatchActivity), so a reprocess that removed
	// the only part of a track inside some region has to clear the old matches first.
	if _, err := tx.Exec(ctx, `DELETE FROM activity_country WHERE activity_id = $1`, activityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM activity_region WHERE activity_id = $1`, activityID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Masks: re-render every tile the new track touches (overwriting in place), then drop
	// the ones it no longer touches. Overwriting rather than clearing first means a tile the
	// track still crosses never goes through a moment with no mask for this activity.
	var newTiles [][2]int
	if points != nil {
		newTiles = computeTouchedTiles(points, FogZoom)
		if err := fog.RenderActivityMasks(ctx, pool, store, activityID, points, newTiles); err != nil {
			return fmt.Errorf("render activity masks: %w", err)
		}
	}
	if err := fog.RemoveActivityMasks(ctx, pool, store, activityID, tilesNotIn(oldTiles, newTiles)); err != nil {
		return fmt.Errorf("remove stale masks: %w", err)
	}

	if points != nil {
		if err := geo.MatchActivity(ctx, pool, activityID); err != nil {
			return fmt.Errorf("match admin boundaries: %w", err)
		}
	}

	// Old ∪ new: a tile the reprocess cut the track out of needs recompositing just as much as
	// one it still crosses. Deduplication is deliberately not re-run (§4.7.7) — this changes
	// the copy's geometry, not which copy of the ride is the real one.
	if err := MarkFogTilesDirty(ctx, pool, userID, mergeTiles(oldTiles, newTiles)); err != nil {
		return fmt.Errorf("mark fog tiles dirty: %w", err)
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

// EnqueueTrackEdit marks the activity pending (and its tiles dirty — markPendingTilesDirty)
// and enqueues its `edit_track` job in one transaction, so an activity is never shown Pending without a job on its way, nor gets a job
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
	if err := markPendingTilesDirty(ctx, tx, job.UserID, []string{job.ActivityID}); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (kind, user_id, payload) VALUES ('edit_track', $1, $2)`,
		job.UserID, payload,
	); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
