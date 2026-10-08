package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// accountPurgeBatchSize bounds one sweep, as demoPurgeBatchSize does; the next tick takes the rest.
const accountPurgeBatchSize = 20

// purgeDeletedAccounts finishes what httpapi's closeAccount started (SPEC FR-1.11): for every
// account with deleted_at set, it removes the account's stored objects, then its users row,
// whose cascade takes every other row it owns — activities, photos, Stories, Private
// locations, captures, imports, jobs.
//
// An account with a job still running (claimed within claimLease) waits for the next tick:
// the job could write a raw file, a tile or a mask after the sweep had already listed them.
// Its pending jobs are dropped first, so nothing new starts for it in the meantime.
//
// Objects first, row second, as demo_purge.go does: a failed removal is logged and the row is
// deleted anyway, since leaving the account in place is worse than an orphaned object. The
// activity masks are keyed by activity id, so those ids are read before the row goes.
//
// Photos are the exception, since a copied Story's photo (IMPLEMENTATION.md §4.23) is another
// account's row over the same files: the account's photo files that another account's rows
// still use are kept, and the files of copies the account received go after its rows, and only
// when nothing else uses them.
func purgeDeletedAccounts(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `SELECT id FROM users WHERE deleted_at IS NOT NULL ORDER BY deleted_at LIMIT $1`, accountPurgeBatchSize)
	if err != nil {
		return fmt.Errorf("account purge: query: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("account purge: scan: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("account purge: rows: %w", err)
	}

	for _, userID := range userIDs {
		if err := purgeAccount(ctx, pool, store, log, userID); err != nil {
			log.Error("account purge failed", "user_id", userID, "err", err)
		}
	}
	return nil
}

func purgeAccount(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, userID string) error {
	if _, err := pool.Exec(ctx, `
		DELETE FROM jobs WHERE user_id = $1 AND state = 'pending'
		  AND (locked_at IS NULL OR locked_at < NOW() - make_interval(secs => $2))
	`, userID, claimLease.Seconds()); err != nil {
		return fmt.Errorf("drop pending jobs: %w", err)
	}
	var running bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM jobs WHERE user_id = $1 AND state = 'pending')`, userID,
	).Scan(&running); err != nil {
		return fmt.Errorf("running jobs: %w", err)
	}
	if running {
		log.Info("account purge: waiting for a running job", "user_id", userID)
		return nil
	}

	activityRows, err := pool.Query(ctx, `SELECT id FROM activities WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("activities: %w", err)
	}
	var prefixes []string
	for activityRows.Next() {
		var id string
		if err := activityRows.Scan(&id); err != nil {
			activityRows.Close()
			return fmt.Errorf("activities: scan: %w", err)
		}
		prefixes = append(prefixes, "activity-masks/"+id+"/")
	}
	activityRows.Close()
	if err := activityRows.Err(); err != nil {
		return fmt.Errorf("activities: rows: %w", err)
	}
	prefixes = append(prefixes,
		"raw/"+userID+"/", "fog/"+userID+"/", "heatmap/"+userID+"/", "exports/"+userID+"/", "imports/"+userID+"/")
	for _, prefix := range prefixes {
		if err := store.RemoveByPrefix(ctx, prefix); err != nil {
			log.Error("account purge: storage cleanup failed, deleting the account anyway", "user_id", userID, "prefix", prefix, "err", err)
		}
	}
	photoPrefix := "photos/" + userID + "/"
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT image_key FROM activity_photos WHERE image_key LIKE $1 || '%' AND user_id <> $2`, photoPrefix, userID)
	if err != nil {
		return fmt.Errorf("shared photos: %w", err)
	}
	shared, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("shared photos: %w", err)
	}
	keep := make(map[string]bool, 2*len(shared))
	for _, key := range shared {
		keep[key], keep[key+"-thumb"] = true, true
	}
	if err := store.RemoveByPrefixExcept(ctx, photoPrefix, func(key string) bool { return keep[key] }); err != nil {
		log.Error("account purge: storage cleanup failed, deleting the account anyway", "user_id", userID, "prefix", photoPrefix, "err", err)
	}
	rows, err = pool.Query(ctx, `
		SELECT DISTINCT image_key FROM activity_photos WHERE user_id = $1 AND image_key NOT LIKE $2 || '%'`, userID, photoPrefix)
	if err != nil {
		return fmt.Errorf("received photos: %w", err)
	}
	received, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("received photos: %w", err)
	}
	// httpapi's avatarKey: one object, no trailing slash to make a prefix of.
	if err := store.Remove(ctx, "avatars/"+userID); err != nil {
		log.Error("account purge: avatar cleanup failed, deleting the account anyway", "user_id", userID, "err", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND deleted_at IS NOT NULL`, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := ingest.RemoveUnreferencedPhotoFiles(ctx, pool, store, received); err != nil {
		log.Error("account purge: received photo cleanup failed", "user_id", userID, "err", err)
	}
	log.Info("account purge: deleted account removed", "user_id", userID)
	return nil
}
