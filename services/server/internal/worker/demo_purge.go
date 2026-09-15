package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/storage"
)

// demoPurgeBatchSize bounds one sweep the same way the job queue bounds one claim loop —
// there is no reason a single tick needs to clear an unbounded backlog; the next tick
// catches whatever's left.
const demoPurgeBatchSize = 100

// purgeExpiredDemoUsers deletes every demo account (users.demo_expires_at,
// migrations/0001_init.sql; VISION.md §8.2) whose expiry has passed. `DELETE FROM users` cascades the
// DB side of this for free — activities, activity_streams, activity_tile_masks, fog_tiles,
// jobs and sessions all reference user_id ON DELETE CASCADE — but object storage has no
// foreign keys, so raw uploads and rendered tile pyramids have to be swept explicitly first.
//
// Known, accepted gap: this sweeps raw/{userID}/, fog/{userID}/ and heatmap/{userID}/ (all
// namespaced by user id — server.go and internal/fog/render.go's object key builders) but
// not activity-masks/{activityID}/, which is namespaced by activity id instead. Those are
// small per-tile crisp masks, not the raw payload or a full tile pyramid — orphaning them
// for a purged demo account is a low-cost simplification, not a correctness bug (nothing
// ever looks them up once activity_tile_masks' own rows are gone with the cascade), and can
// be swept properly later if it ever measures as worth it.
func purgeExpiredDemoUsers(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `
		SELECT id FROM users WHERE demo_expires_at IS NOT NULL AND demo_expires_at < NOW()
		LIMIT $1
	`, demoPurgeBatchSize)
	if err != nil {
		return fmt.Errorf("demo purge: query expired: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("demo purge: scan: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("demo purge: rows: %w", err)
	}

	for _, userID := range userIDs {
		for _, prefix := range []string{"raw/" + userID + "/", "fog/" + userID + "/", "heatmap/" + userID + "/"} {
			if err := store.RemoveByPrefix(ctx, prefix); err != nil {
				log.Error("demo purge: storage cleanup failed, deleting DB row anyway", "user_id", userID, "prefix", prefix, "err", err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			log.Error("demo purge: delete user failed", "user_id", userID, "err", err)
			continue
		}
		log.Info("demo purge: expired demo account removed", "user_id", userID)
	}
	return nil
}
