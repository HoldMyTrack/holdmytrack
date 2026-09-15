package fog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/storage"
)

// RasterKind picks which of the two compositing rules a filtered render uses — the same
// max-vs-sum distinction the unfiltered path's renderAndStoreTile already makes, just per
// call here instead of always doing both at once (a filtered request only ever wants one).
type RasterKind int

const (
	FogKind RasterKind = iota
	HeatmapKind
)

// Filter narrows which activities' masks contribute to a filtered render. Its own type, not
// activityFilter (internal/httpapi/activities.go) — that type has no exclude-id concept, and
// tracks/list/summary have no use for one (they hide client-side via a MapLibre layer
// filter); a raster has no per-feature identity to hide, so exclusion has to reach the server.
type Filter struct {
	From    *time.Time
	To      *time.Time
	Exclude []string
}

// IsUnfiltered reports whether this Filter excludes nothing — the caller's signal to serve
// the cheap precomputed fog_tiles aggregate instead of compositing on the fly.
func (f Filter) IsUnfiltered() bool {
	return f.From == nil && f.To == nil && len(f.Exclude) == 0
}

func (f Filter) cacheKey(userID string, kind RasterKind, zoom, x, y int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%d", userID, kind, zoom, x, y)
	if f.From != nil {
		fmt.Fprintf(h, "|from=%d", f.From.Unix())
	}
	if f.To != nil {
		fmt.Fprintf(h, "|to=%d", f.To.Unix())
	}
	for _, id := range f.Exclude {
		fmt.Fprintf(h, "|x=%s", id)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// filteredCacheTTL bounds how long a composited-on-the-fly tile is reused — real for this
// path (MapLibre issues many overlapping tile requests per pan/zoom, and the same z14 leaves
// get reused across ancestor zooms within one session), not needed for the unfiltered path,
// which is still served straight from fog_tiles with no caching layer of its own.
const filteredCacheTTL = 45 * time.Second

type cacheEntry struct {
	mask    *image.Gray
	expires time.Time
}

var (
	filteredCacheMu sync.Mutex
	filteredCache   = map[string]cacheEntry{}
)

func cacheGet(key string) (*image.Gray, bool) {
	filteredCacheMu.Lock()
	defer filteredCacheMu.Unlock()
	e, ok := filteredCache[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.mask, true
}

func cacheSet(key string, mask *image.Gray) {
	filteredCacheMu.Lock()
	defer filteredCacheMu.Unlock()
	// Opportunistic sweep rather than a background goroutine — this map only ever grows from
	// live tile requests, and clearing expired entries on the next write is enough to keep it
	// bounded without a separate timer to manage.
	now := time.Now()
	for k, e := range filteredCache {
		if now.After(e.expires) {
			delete(filteredCache, k)
		}
	}
	filteredCache[key] = cacheEntry{mask: mask, expires: now.Add(filteredCacheTTL)}
}

// RenderFilteredTile computes the requested (zoom,x,y) tile on the fly, honoring filter, by
// compositing only the per-activity z14 masks that pass it and downsampling to the requested
// zoom if needed — never touching raw GPS payloads, unlike the pre-per-activity-mask design.
//
// Three things keep this from scaling with 4^(14-zoom) at low zoom, addressed in order:
//  1. A single existence check against the *unfiltered* fog_tiles aggregate — filtering only
//     ever removes activities relative to it, so nothing there unfiltered means nothing there
//     filtered either. Makes the overwhelmingly common "no coverage in this part of the
//     world" case free.
//  2. One bounded range query over activity_tile_masks for every z14 descendant of the
//     requested tile — a contiguous rectangle, one indexed query, not one call per node.
//  3. All downsampling from there happens in memory (downsampleQuadrants, same as the
//     precomputed pyramid), not further DB round trips.
func RenderFilteredTile(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, kind RasterKind, zoom, x, y int, filter Filter) (*image.Gray, error) {
	cacheKey := filter.cacheKey(userID, kind, zoom, x, y)
	if mask, ok := cacheGet(cacheKey); ok {
		return mask, nil
	}

	var unfilteredExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4 AND object_key IS NOT NULL)
	`, userID, zoom, x, y).Scan(&unfilteredExists); err != nil {
		return nil, fmt.Errorf("fog: filtered pre-check: %w", err)
	}
	if !unfilteredExists {
		return blankTile(), nil
	}

	span := 1 << (Zoom - zoom)
	x0, x1 := x*span, x*span+span-1
	y0, y1 := y*span, y*span+span-1

	rows, err := pool.Query(ctx, `
		SELECT m.tile_x, m.tile_y, m.mask_object_key
		FROM activity_tile_masks m
		JOIN activities a ON a.id = m.activity_id
		WHERE a.user_id = $1 AND m.zoom = $2
		  AND m.tile_x BETWEEN $3 AND $4 AND m.tile_y BETWEEN $5 AND $6
		  AND ($7::timestamptz IS NULL OR a.started_at >= $7)
		  AND ($8::timestamptz IS NULL OR a.started_at <= $8)
		  AND ($9::text[] IS NULL OR NOT (a.id::text = ANY($9)))
	`, userID, Zoom, x0, x1, y0, y1, filter.From, filter.To, nilIfEmpty(filter.Exclude))
	if err != nil {
		return nil, fmt.Errorf("fog: filtered mask query: %w", err)
	}

	leafMaskKeys := map[[2]int][]string{}
	for rows.Next() {
		var tx, ty int
		var key string
		if err := rows.Scan(&tx, &ty, &key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("fog: scan filtered mask row: %w", err)
		}
		leafMaskKeys[[2]int{tx, ty}] = append(leafMaskKeys[[2]int{tx, ty}], key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	leaves := make(map[[2]int]*image.Gray, len(leafMaskKeys))
	for coord, keys := range leafMaskKeys {
		masks := make([]*image.Gray, 0, len(keys))
		for _, key := range keys {
			mask, err := loadCrispMask(ctx, store, key)
			if err != nil {
				// Same "one bad file doesn't abort the batch" principle as the unfiltered
				// path — skip this one activity's contribution, not the whole tile.
				continue
			}
			masks = append(masks, mask)
		}
		leaves[coord] = compositeLeaf(kind, masks)
	}

	result := buildLevel(leaves, zoom, x, y)
	cacheSet(cacheKey, result)
	return result, nil
}

func compositeLeaf(kind RasterKind, masks []*image.Gray) *image.Gray {
	if kind == HeatmapKind {
		return compositeHeatmapMask(masks)
	}
	return compositeFogMask(masks)
}

// buildLevel assembles the requested (targetZoom,targetX,targetY) tile from the sparse leaf
// map, built *bottom-up* one level at a time from whichever nodes actually exist — never a
// naive top-down recursion that would visit all 4^(Zoom-targetZoom) theoretical descendants
// at low zoom regardless of how few of them have any real data. Each level up only
// constructs parents that have at least one non-blank child (missing children fall back to
// blankTile(), same convention the precomputed pyramid's loadTileImage uses), so total work
// stays proportional to how many z14 tiles this filter actually touched, not to zoom depth.
func buildLevel(leaves map[[2]int]*image.Gray, targetZoom, targetX, targetY int) *image.Gray {
	current := leaves
	for z := Zoom; z > targetZoom; z-- {
		parentCoords := map[[2]int]struct{}{}
		for coord := range current {
			parentCoords[[2]int{coord[0] / 2, coord[1] / 2}] = struct{}{}
		}
		next := make(map[[2]int]*image.Gray, len(parentCoords))
		for p := range parentCoords {
			coords := [4][2]int{{p[0] * 2, p[1] * 2}, {p[0]*2 + 1, p[1] * 2}, {p[0] * 2, p[1]*2 + 1}, {p[0]*2 + 1, p[1]*2 + 1}}
			var children [4]*image.Gray
			for i, c := range coords {
				if child, ok := current[c]; ok {
					children[i] = child
				} else {
					children[i] = blankTile()
				}
			}
			next[p] = downsampleQuadrants(children)
		}
		current = next
	}
	if result, ok := current[[2]int{targetX, targetY}]; ok {
		return result
	}
	return blankTile()
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
