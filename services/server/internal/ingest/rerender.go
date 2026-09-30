package ingest

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// RerenderCoverage rebuilds every Fog and Heatmap tile of one account, or of every account with
// any (userID ""), for when how tiles are drawn has changed rather than what's in them — the
// `rerender-coverage` subcommand, run once on deploying such a change (docs/DEPLOY.md). It marks
// each account's z14 tiles dirty and queues one render_fog job per account: the worker then
// re-renders them and their whole pyramid, and bumps the account's tile version, so browsers
// drop the tiles they cached. Queued rather than rendered here, so it never races the worker
// over the same tiles. Idempotent: running it twice only renders twice.
//
// masks also redraws every activity's stored per-tile masks first (RerenderActivityMasks) —
// needed when the stroke itself changes (fog's strokeRadiusPx), not for a change to how masks
// are composited or coloured. It re-parses every activity's upload, so it takes a while.
func RerenderCoverage(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, userID string, masks bool) error {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT user_id FROM fog_tiles
		WHERE zoom = $1 AND ($2 = '' OR user_id::text = $2)
	`, FogZoom, userID)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	var users []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		users = append(users, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range users {
		if masks {
			if err := rerenderUserMasks(ctx, pool, store, log, id); err != nil {
				return fmt.Errorf("masks for %s: %w", id, err)
			}
		}
		tag, err := pool.Exec(ctx, `UPDATE fog_tiles SET dirty = true WHERE user_id = $1 AND zoom = $2`, id, FogZoom)
		if err != nil {
			return fmt.Errorf("mark %s dirty: %w", id, err)
		}
		if err := EnqueueRenderFog(ctx, pool, id); err != nil {
			return fmt.Errorf("enqueue render for %s: %w", id, err)
		}
		log.Info("rerender-coverage: queued", "user", id, "tiles", tag.RowsAffected())
	}
	log.Info("rerender-coverage: done", "accounts", len(users))
	return nil
}

// rerenderUserMasks redraws every activity of one account whose masks can be redrawn. One that
// can't — Pending (its own reprocess will), or with no upload stored — is logged and left as it
// is rather than failing the whole run.
func rerenderUserMasks(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, userID string) error {
	rows, err := pool.Query(ctx, `SELECT id FROM activities WHERE user_id = $1 ORDER BY started_at`, userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := RerenderActivityMasks(ctx, pool, store, userID, id); err != nil {
			log.Warn("rerender-coverage: masks skipped", "activity", id, "err", err)
		}
	}
	log.Info("rerender-coverage: masks redrawn", "user", userID, "activities", len(ids))
	return nil
}

// RerenderActivityMasks redraws one activity's stored per-tile masks from the points it's shown
// with (DisplayedPoints: the upload parsed, clipped to Private locations, its track edit applied)
// at the current stroke, drops the masks of tiles it no longer reaches, and marks every tile
// before or after dirty — reprocessActivity's mask step alone, since nothing else about the
// activity changes. Superseded activities too, so one promoted later is drawn the same.
func RerenderActivityMasks(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, activityID string) error {
	points, err := DisplayedPoints(ctx, pool, store, activityID)
	if err != nil {
		return err
	}
	oldTiles, err := ActivityTiles(ctx, pool, activityID)
	if err != nil {
		return err
	}
	var newTiles [][2]int
	if points != nil {
		newTiles = computeTouchedTiles(points, FogZoom)
		if err := fog.RenderActivityMasks(ctx, pool, store, activityID, points, newTiles); err != nil {
			return fmt.Errorf("render masks: %w", err)
		}
	}
	if err := fog.RemoveActivityMasks(ctx, pool, store, activityID, tilesNotIn(oldTiles, newTiles)); err != nil {
		return fmt.Errorf("remove stale masks: %w", err)
	}
	return MarkFogTilesDirty(ctx, pool, userID, mergeTiles(oldTiles, newTiles))
}
