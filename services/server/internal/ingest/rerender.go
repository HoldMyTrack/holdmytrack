package ingest

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RerenderCoverage rebuilds every Fog and Heatmap tile of one account, or of every account with
// any (userID ""), for when how tiles are drawn has changed rather than what's in them — the
// `rerender-coverage` subcommand, run once on deploying such a change (docs/DEPLOY.md). It marks
// each account's z14 tiles dirty and queues one render_fog job per account: the worker then
// re-renders them and their whole pyramid, and bumps the account's tile version, so browsers
// drop the tiles they cached. Queued rather than rendered here, so it never races the worker
// over the same tiles. Idempotent: running it twice only renders twice.
func RerenderCoverage(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, userID string) error {
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
