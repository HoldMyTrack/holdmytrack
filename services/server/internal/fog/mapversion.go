package fog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// execer is what BumpMapVersion writes through — a pool or a transaction, so a caller that
// already has one open bumps inside it.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// BumpMapVersion advances users.map_version, the key every per-user map tile is cached under
// (TileVersion, IMPLEMENTATION.md §4.2's "Caching"). Call it after any change that alters
// what a Fog, Heatmap, Country/Region or tracks tile would return. A change the worker makes
// is covered by the bump at the end of RenderUser; one made at request time, which the next
// render may be seconds away from (or never follow, like a type edit), bumps here.
func BumpMapVersion(ctx context.Context, db execer, userID string) error {
	_, err := db.Exec(ctx, `UPDATE users SET map_version = map_version + 1 WHERE id = $1`, userID)
	return err
}

// TileVersion is the opaque cache key clients send as a tile's `cv` parameter: the account's
// own id prefix, so two accounts signed in one after the other on the same browser or phone
// never share a cached tile, and its map_version.
func TileVersion(userID string, mapVersion int64) string {
	return fmt.Sprintf("%s.%d", tileVersionPrefix(userID), mapVersion)
}

// TileVersionOwnedBy reports whether cv is one of userID's own tile versions — the only kind a
// tile response may be cached under, so one account's tiles can never be stored under another's
// key.
func TileVersionOwnedBy(cv, userID string) bool {
	prefix := tileVersionPrefix(userID) + "."
	return len(cv) > len(prefix) && strings.HasPrefix(cv, prefix)
}

func tileVersionPrefix(userID string) string {
	if len(userID) > 8 {
		return userID[:8]
	}
	return userID
}
