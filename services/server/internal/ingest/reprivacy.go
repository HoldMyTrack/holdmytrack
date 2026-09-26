package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// ReprivacyJob is the `reprivacy` job's payload (IMPLEMENTATION.md §7): the activities one
// Private location change may have affected, to be reprocessed against the account's
// current set of locations.
type ReprivacyJob struct {
	UserID      string   `json:"user_id"`
	ActivityIDs []string `json:"activity_ids"`
}

// zoneMarginM widens the affected-set search past a location's own radius. The stored
// trajectory is simplified (a few metres of tolerance), and a track clipped by a location
// ends exactly on its edge — both must still count as touching it.
const zoneMarginM = 25

// AffectedActivities returns the live activities of userID whose display trajectory comes
// within a circle's radius (plus zoneMarginM) of its center: every activity a location placed,
// or previously placed, there could clip. Run inside the caller's transaction, alongside the
// privacy_zones change itself.
func AffectedActivities(ctx context.Context, tx pgx.Tx, userID string, lat, lon float64, radiusM int) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM activities
		WHERE user_id = $1 AND superseded_by IS NULL AND raw_payload_key IS NOT NULL
		  AND trajectory IS NOT NULL
		  AND ST_DWithin(trajectory::geography, ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography, $4)
	`, userID, lat, lon, radiusM+zoneMarginM)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AffectedHiddenActivities is AffectedActivities for the activities a location change can
// bring back but that have no trajectory to search by — those entirely inside Private
// locations. There are few, and reprocessing one that stays hidden changes nothing.
func AffectedHiddenActivities(ctx context.Context, tx pgx.Tx, userID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM activities
		WHERE user_id = $1 AND superseded_by IS NULL AND raw_payload_key IS NOT NULL
		  AND trajectory IS NULL
	`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// EnqueueReprivacy marks every given activity Pending — the same flag, and the same Activity
// List badge, as a track edit (§4.7.7) — and enqueues one `reprivacy` job for all of them,
// inside the caller's transaction so a location is never saved without its reprocessing on
// the way. Unlike EnqueueTrackEdit it doesn't skip an activity already pending: an edit in
// flight was clipped with the old locations, and this job, queued behind it, fixes that.
// Their tiles are marked dirty too, so the job's first render drops them from Fog/Heatmap
// (markPendingTilesDirty).
func EnqueueReprivacy(ctx context.Context, tx pgx.Tx, job ReprivacyJob) error {
	if len(job.ActivityIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE activities SET edit_pending = true WHERE user_id = $1 AND id = ANY($2::uuid[])
	`, job.UserID, job.ActivityIDs); err != nil {
		return err
	}
	if err := markPendingTilesDirty(ctx, tx, job.UserID, job.ActivityIDs); err != nil {
		return err
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind, user_id, payload) VALUES ('reprivacy', $1, $2)`, job.UserID, payload)
	return err
}

// ProcessReprivacy reprocesses a batch of activities against the account's current Private
// locations. It renders twice, each time every dirty tile once — not one composite per
// activity, which for a location at home would redo the same busy tiles hundreds of times:
// first before reprocessing, which drops the batch (Pending, and so left out of the render)
// from Fog/Heatmap; then after clearing Pending, which brings it back with its new masks.
// Pending therefore clears just before the second render; the client refreshes Fog/Heatmap
// off the coverage status (this job counts as rendering until it returns), not off the badge.
//
// One activity failing doesn't stop the rest; the job still fails afterwards, naming them.
// Pending is cleared for failed ones too, rather than left stuck on.
func ProcessReprivacy(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, jobID int64, job ReprivacyJob) error {
	if err := fog.RenderUser(ctx, pool, store, job.UserID); err != nil {
		return fmt.Errorf("reprivacy: render fog/heatmap: %w", err)
	}

	var failed []string
	for _, id := range job.ActivityIDs {
		if err := reprocessActivity(ctx, pool, store, job.UserID, id, nil); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", id, err))
		}
	}

	// A later location change still queued for any of these will reprocess it again — leave
	// its badge on until that one lands.
	if _, err := pool.Exec(ctx, `
		UPDATE activities a SET edit_pending = false
		WHERE a.user_id = $1 AND a.id = ANY($2::uuid[])
		  AND NOT EXISTS (
			SELECT 1 FROM jobs j
			WHERE j.user_id = $1 AND j.kind = 'reprivacy' AND j.state = 'pending' AND j.id > $3
			  AND j.payload->'activity_ids' ? a.id::text
		  )
	`, job.UserID, job.ActivityIDs, jobID); err != nil {
		return fmt.Errorf("reprivacy: clear edit_pending: %w", err)
	}
	if err := fog.RenderUser(ctx, pool, store, job.UserID); err != nil {
		return fmt.Errorf("reprivacy: render fog/heatmap: %w", err)
	}

	if len(failed) > 0 {
		return fmt.Errorf("reprivacy: %d of %d activities failed: %s", len(failed), len(job.ActivityIDs), strings.Join(failed, "; "))
	}
	return nil
}

// markPendingTilesDirty flags every z14 tile the given activities' current masks cover, inside
// the transaction that marks them Pending — so the reprocess job's first render leaves them out
// of Fog/Heatmap, and the tiles are already dirty however soon that job runs. The same upsert
// as MarkFogTilesDirty, fed straight from activity_tile_masks rather than a tile list.
func markPendingTilesDirty(ctx context.Context, tx pgx.Tx, userID string, activityIDs []string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty)
		SELECT DISTINCT $1::uuid, m.zoom, m.tile_x, m.tile_y, true
		FROM activity_tile_masks m
		WHERE m.activity_id = ANY($2::uuid[]) AND m.zoom = $3
		ON CONFLICT (user_id, zoom, tile_x, tile_y) DO UPDATE SET dirty = true
	`, userID, activityIDs, FogZoom)
	return err
}
