package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// RemoveUnreferencedPhotoFiles removes the image and thumbnail at each of keys that no
// activity_photos row refers to any more (IMPLEMENTATION.md §4.27). A copied Story's photos
// (§4.23) are rows of the recipient's over the original's files, so a photo's files can outlive
// the row that first stored them: callers delete rows first and pass the keys those rows had.
//
// A file still in use is kept whoever's row uses it. Removal is best-effort: every key is
// tried, and the first failure is returned for the caller to log.
func RemoveUnreferencedPhotoFiles(ctx context.Context, db Querier, store *storage.Store, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
		SELECT k FROM unnest($1::text[]) AS k
		WHERE NOT EXISTS (SELECT 1 FROM activity_photos WHERE image_key = k)`, keys)
	if err != nil {
		return fmt.Errorf("photo files: reference check: %w", err)
	}
	unused, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("photo files: reference check: %w", err)
	}
	var errs []error
	for _, key := range unused {
		for _, k := range []string{key, key + "-thumb"} {
			if err := store.Remove(ctx, k); err != nil {
				errs = append(errs, fmt.Errorf("photo files: remove %s: %w", k, err))
			}
		}
	}
	return errors.Join(errs...)
}
