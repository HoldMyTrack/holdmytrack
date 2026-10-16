package stats

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/platform/services/server/internal/storage"
)

// backfillBatch is how many sizes one UPDATE writes.
const backfillBatch = 1000

// BackfillObjectBytes is the one-off `backfill-object-bytes` (docs/DEPLOY.md): it fills
// activities.raw_bytes and fog_tiles' sizes for rows written before migration 0039 recorded
// them, from listings of the bucket rather than a request per object. Only a NULL size is
// written, so running it again, or beside a worker that is rendering, changes nothing already
// measured. It returns how many sizes it wrote.
func BackfillObjectBytes(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) (int64, error) {
	raw, err := backfillRaw(ctx, pool, store)
	if err != nil {
		return raw, err
	}
	log.Info("backfill-object-bytes: originals", "filled", raw)
	tiles, err := backfillTiles(ctx, pool, store)
	log.Info("backfill-object-bytes: tiles", "filled", tiles)
	return raw + tiles, err
}

// backfillRaw fills raw_bytes. Activities share an original (split pieces), so it reads the
// unmeasured rows' ids by key first and updates them by id.
func backfillRaw(ctx context.Context, pool *pgxpool.Pool, store *storage.Store) (int64, error) {
	rows, err := pool.Query(ctx, `
		SELECT raw_payload_key, array_agg(id::text) FROM activities
		WHERE raw_payload_key IS NOT NULL AND raw_bytes IS NULL
		GROUP BY raw_payload_key`)
	if err != nil {
		return 0, fmt.Errorf("backfill: unmeasured originals: %w", err)
	}
	byKey := map[string][]string{}
	for rows.Next() {
		var key string
		var ids []string
		if err := rows.Scan(&key, &ids); err != nil {
			rows.Close()
			return 0, fmt.Errorf("backfill: unmeasured originals: %w", err)
		}
		byKey[key] = ids
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("backfill: unmeasured originals: %w", err)
	}
	if len(byKey) == 0 {
		return 0, nil
	}

	var filled int64
	var ids []string
	var sizes []int64
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		tag, err := pool.Exec(ctx, `
			UPDATE activities a SET raw_bytes = t.s
			FROM unnest($1::uuid[], $2::bigint[]) AS t(id, s)
			WHERE a.id = t.id AND a.raw_bytes IS NULL`, ids, sizes)
		if err != nil {
			return fmt.Errorf("backfill: write originals: %w", err)
		}
		filled += tag.RowsAffected()
		ids, sizes = ids[:0], sizes[:0]
		return nil
	}
	err = store.List(ctx, "raw/", func(key string, size int64) error {
		for _, id := range byKey[key] {
			ids = append(ids, id)
			sizes = append(sizes, size)
		}
		if len(ids) >= backfillBatch {
			return flush()
		}
		return nil
	})
	if err != nil {
		return filled, err
	}
	return filled, flush()
}

// tileKey is a Fog or Heatmap tile's object key, read back into the fog_tiles row it belongs
// to: "fog/{user}/{z}/{x}/{y}.png" or "heatmap/…" (fog.fogObjectKey, fog.heatmapObjectKey).
// Anything else under those prefixes is skipped.
type tileKey struct {
	userID  string
	z, x, y int
}

func parseTileKey(key string) (tileKey, bool) {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || (parts[0] != "fog" && parts[0] != "heatmap") || !isUUID(parts[1]) {
		return tileKey{}, false
	}
	last, ok := strings.CutSuffix(parts[4], ".png")
	if !ok {
		return tileKey{}, false
	}
	var n [3]int
	for i, s := range []string{parts[2], parts[3], last} {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			return tileKey{}, false
		}
		n[i] = v
	}
	return tileKey{userID: parts[1], z: n[0], x: n[1], y: n[2]}, true
}

// isUUID reports whether s is a UUID in its usual text form, which the UPDATE casts it to.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// backfillTiles fills fog_tiles' bytes and heatmap_bytes, by the primary key each object's key
// names. A row is updated only while its key is still this object's and its size is unmeasured.
func backfillTiles(ctx context.Context, pool *pgxpool.Pool, store *storage.Store) (int64, error) {
	var filled int64
	for _, layer := range []struct{ prefix, keyCol, sizeCol string }{
		{"fog/", "object_key", "bytes"},
		{"heatmap/", "heatmap_object_key", "heatmap_bytes"},
	} {
		var users, keys []string
		var zs, xs, ys []int32
		var sizes []int64
		flush := func() error {
			if len(keys) == 0 {
				return nil
			}
			tag, err := pool.Exec(ctx, `
				UPDATE fog_tiles f SET `+layer.sizeCol+` = t.s
				FROM unnest($1::uuid[], $2::smallint[], $3::int[], $4::int[], $5::text[], $6::bigint[]) AS t(u, z, x, y, k, s)
				WHERE f.user_id = t.u AND f.zoom = t.z AND f.tile_x = t.x AND f.tile_y = t.y
				  AND f.`+layer.keyCol+` = t.k AND f.`+layer.sizeCol+` IS NULL`,
				users, zs, xs, ys, keys, sizes)
			if err != nil {
				return fmt.Errorf("backfill: write %s sizes: %w", layer.prefix, err)
			}
			filled += tag.RowsAffected()
			users, zs, xs, ys, keys, sizes = users[:0], zs[:0], xs[:0], ys[:0], keys[:0], sizes[:0]
			return nil
		}
		err := store.List(ctx, layer.prefix, func(key string, size int64) error {
			t, ok := parseTileKey(key)
			if !ok {
				return nil
			}
			users, zs, xs, ys = append(users, t.userID), append(zs, int32(t.z)), append(xs, int32(t.x)), append(ys, int32(t.y))
			keys, sizes = append(keys, key), append(sizes, size)
			if len(keys) >= backfillBatch {
				return flush()
			}
			return nil
		})
		if err != nil {
			return filled, err
		}
		if err := flush(); err != nil {
			return filled, err
		}
	}
	return filled, nil
}
