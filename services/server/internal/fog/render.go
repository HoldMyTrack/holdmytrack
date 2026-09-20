package fog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/parse"
	"github.com/fitmap/fitmap/services/server/internal/storage"
	"github.com/fitmap/fitmap/services/server/internal/tilemath"
)

// RenderUser is the `render_fog` job body: re-render every z14 tile currently marked dirty
// for this user, then walk the pyramid upward from whatever changed. Idempotent and
// complete per tile, not incremental — each render gathers *every* activity intersecting a
// tile, not just whichever one triggered the dirty flag, because a tile's mask has to
// represent the user's entire history through it every time, not just the newest activity.
func RenderUser(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string) error {
	dirty, err := dirtyTiles(ctx, pool, userID, Zoom)
	if err != nil {
		return fmt.Errorf("fog: list dirty tiles: %w", err)
	}

	changed := make(map[[2]int]struct{}, len(dirty))
	for _, t := range dirty {
		if err := renderAndStoreTile(ctx, pool, store, userID, Zoom, t[0], t[1]); err != nil {
			return fmt.Errorf("fog: render z%d/%d/%d: %w", Zoom, t[0], t[1], err)
		}
		changed[t] = struct{}{}
	}

	// Pyramid: z13 down to z0, each level built from whichever tiles at the level below
	// actually changed. Above z14 the client overzooms the z14 raster directly (§4.2) —
	// no pyramid needed there.
	for z := Zoom - 1; z >= 0 && len(changed) > 0; z-- {
		parents := map[[2]int]struct{}{}
		for t := range changed {
			parents[[2]int{t[0] / 2, t[1] / 2}] = struct{}{}
		}
		next := make(map[[2]int]struct{}, len(parents))
		for p := range parents {
			if err := renderPyramidLevel(ctx, pool, store, userID, z, p[0], p[1]); err != nil {
				return fmt.Errorf("fog: render pyramid z%d/%d/%d: %w", z, p[0], p[1], err)
			}
			next[p] = struct{}{}
		}
		changed = next
	}
	return nil
}

// Privacy trim is no longer re-applied here: RenderActivityMasks renders from points already
// trimmed once at ingest, and renderAndStoreTile now composites already-rendered masks
// rather than re-parsing raw payloads with the user's *current* trim setting. This is a
// latent gap for a feature that doesn't exist yet (there is no way to change
// `users.privacy_trim_m` today, and no reprivacy job — §7 — reads it back): when one is
// built, it needs to re-call RenderActivityMasks with freshly re-trimmed points per affected
// activity, not just re-render a tile from its existing masks.

func dirtyTiles(ctx context.Context, pool *pgxpool.Pool, userID string, zoom int) ([][2]int, error) {
	rows, err := pool.Query(ctx,
		`SELECT tile_x, tile_y FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND dirty`,
		userID, zoom,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][2]int
	for rows.Next() {
		var x, y int
		if err := rows.Scan(&x, &y); err != nil {
			return nil, err
		}
		out = append(out, [2]int{x, y})
	}
	return out, rows.Err()
}

// renderAndStoreTile is one z14 tile, fully re-rendered from the per-activity masks already
// on file for it — no raw-payload re-fetch/re-parse here any more (see RenderActivityMasks,
// which is what actually does that, once per activity, at ingest time). This is also more
// precise than the old bbox-intersection query it replaces: activity_tile_masks only ever
// contains tiles the *raw* trajectory actually passed through, not an approximation from the
// simplified trajectory's bounding box.
//
// Fog and Heatmap draw from the same query but not the same *rows*: Fog is a true all-time
// aggregate (every non-superseded activity's mask), while Heatmap only composites masks whose
// activity falls inside the rolling window (HeatmapWindowDays) as of this render — an activity
// that ages past it stops contributing the next time this tile is re-rendered. That "next
// time" is driven by two triggers: the normal ingest/delete dirty-marking (immediate, for
// anything that actually changed), and a weekly sweep (internal/worker's heatmapRefresh) that
// marks every tile dirty purely so the window's trailing edge keeps moving even when nothing
// new is uploaded — see that file's own comment for why weekly, not live-per-request, is the
// right granularity for an aging effect nobody is watching in real time.
func renderAndStoreTile(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int) error {
	windowStart := time.Now().AddDate(0, 0, -HeatmapWindowDays)
	rows, err := pool.Query(ctx, `
		SELECT m.mask_object_key, a.started_at
		FROM activity_tile_masks m
		JOIN activities a ON a.id = m.activity_id
		WHERE a.user_id = $1 AND a.superseded_by IS NULL
		  AND m.zoom = $2 AND m.tile_x = $3 AND m.tile_y = $4
	`, userID, zoom, x, y)
	if err != nil {
		return fmt.Errorf("query activity masks: %w", err)
	}
	type keyedMask struct {
		key       string
		startedAt time.Time
	}
	var rowsOut []keyedMask
	for rows.Next() {
		var row keyedMask
		if err := rows.Scan(&row.key, &row.startedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan activity mask key: %w", err)
		}
		rowsOut = append(rowsOut, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	fogMasks := make([]*image.Gray, 0, len(rowsOut))
	heatmapMasks := make([]*image.Gray, 0, len(rowsOut))
	for _, row := range rowsOut {
		mask, err := loadCrispMask(ctx, store, row.key)
		if err != nil {
			// One missing/corrupt mask should not fail the whole tile — every other
			// activity through it is still real coverage. Skip and continue, the same
			// "one bad file doesn't abort the batch" principle §5.1 applies to bulk
			// ingest.
			continue
		}
		fogMasks = append(fogMasks, mask)
		if row.startedAt.After(windowStart) {
			heatmapMasks = append(heatmapMasks, mask)
		}
	}

	// Both rasters composite the same shape of input differently (§4.2.2: max vs sum) — see
	// compositeFogMask/compositeHeatmapMask in raster.go — but, per the window above, not
	// necessarily the same *set* of masks.
	fogMask := compositeFogMask(fogMasks)
	heatmapMask := compositeHeatmapMask(heatmapMasks)
	if err := storeTilePNG(ctx, store, fogObjectKey(userID, zoom, x, y), fogMask); err != nil {
		return err
	}
	if err := storeTilePNG(ctx, store, heatmapObjectKey(userID, zoom, x, y), heatmapMask); err != nil {
		return err
	}
	return upsertTileRendered(ctx, pool, userID, zoom, x, y,
		fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y))
}

// loadCrispMask fetches and decodes one activity's stored crisp (unblurred) mask —
// activity_tile_masks' own object, read back here by renderAndStoreTile to build both the
// Fog and Heatmap aggregates for one tile.
func loadCrispMask(ctx context.Context, store *storage.Store, key string) (*image.Gray, error) {
	obj, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	img, err := png.Decode(obj)
	if err != nil {
		return nil, err
	}
	if gray, ok := img.(*image.Gray); ok {
		return gray, nil
	}
	b := img.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, img, b.Min, draw.Src)
	return gray, nil
}

// RenderActivityMasks renders and stores exactly one activity's own crisp (unblurred) stroke
// mask for each of its already-computed touched z14 tiles (internal/ingest's
// computeTouchedTiles), then upserts one activity_tile_masks row per tile. Idempotent: safe
// to re-run for the same activity (a redelivered ingest job).
//
// Called from ingest.Process with the points already in memory (pre-simplification, already
// privacy-trimmed) — unlike the old per-tile re-render this replaces, this never reads raw
// payloads back from object storage for the activity that triggered it. Ingest-time
// rendering cost is now proportional to just this one activity's own tiles, independent of
// how many *other* activities already touch them — the old design re-parsed every one of
// them on every tile they shared.
func RenderActivityMasks(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, activityID string, points []parse.Point, tiles [][2]int) error {
	if len(tiles) == 0 {
		return nil
	}

	xs := make([]int32, 0, len(tiles))
	ys := make([]int32, 0, len(tiles))
	keys := make([]string, 0, len(tiles))
	for _, t := range tiles {
		x, y := t[0], t[1]
		mask := renderActivityMask(projectToTile(points, x, y, Zoom))
		key := activityMaskObjectKey(activityID, Zoom, x, y)
		if err := storeTilePNG(ctx, store, key, mask); err != nil {
			return fmt.Errorf("store activity mask z%d/%d/%d: %w", Zoom, x, y, err)
		}
		xs = append(xs, int32(x))
		ys = append(ys, int32(y))
		keys = append(keys, key)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO activity_tile_masks (activity_id, zoom, tile_x, tile_y, mask_object_key, rendered_at)
		SELECT $1, $2, x, y, key, NOW()
		FROM unnest($3::int[], $4::int[], $5::text[]) AS t(x, y, key)
		ON CONFLICT (activity_id, zoom, tile_x, tile_y)
		DO UPDATE SET mask_object_key = EXCLUDED.mask_object_key, rendered_at = NOW()
	`, activityID, Zoom, xs, ys, keys)
	return err
}

func activityMaskObjectKey(activityID string, zoom, x, y int) string {
	return fmt.Sprintf("activity-masks/%s/%d/%d/%d.png", activityID, zoom, x, y)
}

func projectToTile(points []parse.Point, tileX, tileY, zoom int) []pixelPoint {
	originX := float64(tileX) * TileSize
	originY := float64(tileY) * TileSize
	out := make([]pixelPoint, len(points))
	for i, p := range points {
		wx, wy := tilemath.WorldPixel(p.Lon, p.Lat, zoom, TileSize)
		out[i] = pixelPoint{x: wx - originX, y: wy - originY}
	}
	return out
}

// renderPyramidLevel builds one parent tile by downsampling its four children — each
// loaded from whatever is currently stored for it (or blank, if it has never been
// rendered), not just the children that happened to change this pass. A parent has to be a
// complete, correct downsample of its children's *current* state every time, the same
// completeness requirement renderAndStoreTile has at z14. Both rasters' pyramids are built
// here together — they change together, from the same z14 renders, so there is no case
// where one needs a pyramid rebuild and the other doesn't.
func renderPyramidLevel(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int) error {
	coords := [4][2]int{{x * 2, y * 2}, {x*2 + 1, y * 2}, {x * 2, y*2 + 1}, {x*2 + 1, y*2 + 1}}
	var fogChildren, heatmapChildren [4]*image.Gray
	for i, c := range coords {
		fogImg, err := loadTileImage(ctx, pool, store, userID, zoom+1, c[0], c[1], "object_key")
		if err != nil {
			return err
		}
		fogChildren[i] = fogImg
		heatmapImg, err := loadTileImage(ctx, pool, store, userID, zoom+1, c[0], c[1], "heatmap_object_key")
		if err != nil {
			return err
		}
		heatmapChildren[i] = heatmapImg
	}

	fogMask := downsampleQuadrants(fogChildren)
	heatmapMask := downsampleQuadrants(heatmapChildren)
	if err := storeTilePNG(ctx, store, fogObjectKey(userID, zoom, x, y), fogMask); err != nil {
		return err
	}
	if err := storeTilePNG(ctx, store, heatmapObjectKey(userID, zoom, x, y), heatmapMask); err != nil {
		return err
	}
	return upsertTileRendered(ctx, pool, userID, zoom, x, y,
		fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y))
}

// loadTileImage fetches a tile's currently-stored mask (fog or heatmap, per column) or a
// blank one if the tile has no fog_tiles row yet or hasn't been rendered on that side — the
// correct stand-in for "nothing here", not an error, since a user's history need not touch
// every sibling tile. A blank mask is all-zero either way; RenderFogPNG and RenderHeatmapPNG
// each interpret zero correctly for their own mode (fully fogged vs. fully transparent).
//
// column is a Go identifier, not user input — always one of the two literal call sites
// below, never interpolated from a request.
func loadTileImage(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int, column string) (*image.Gray, error) {
	var key *string
	err := pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`, column),
		userID, zoom, x, y,
	).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) || key == nil {
		return blankTile(), nil
	}
	if err != nil {
		return nil, err
	}

	obj, err := store.Get(ctx, *key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	img, err := png.Decode(obj)
	if err != nil {
		return nil, err
	}
	if gray, ok := img.(*image.Gray); ok {
		return gray, nil
	}
	// Defensive: png.Decode should hand back *image.Gray for the grayscale PNGs this
	// package itself writes, but convert explicitly rather than assume the concrete type.
	b := img.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, img, b.Min, draw.Src)
	return gray, nil
}

func storeTilePNG(ctx context.Context, store *storage.Store, key string, mask *image.Gray) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, mask); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	return store.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

func fogObjectKey(userID string, zoom, x, y int) string {
	return fmt.Sprintf("fog/%s/%d/%d/%d.png", userID, zoom, x, y)
}

func heatmapObjectKey(userID string, zoom, x, y int) string {
	return fmt.Sprintf("heatmap/%s/%d/%d/%d.png", userID, zoom, x, y)
}

// upsertTileRendered records a fresh render of *both* rasters in one write. INSERT ...
// ON CONFLICT, not a plain UPDATE: a pyramid tile (z13..z0) may have no fog_tiles row yet
// the first time it's built, unlike a z14 tile, which always has one already from ingest's
// dirty-marking upsert. One write, not two, because the two rasters are never rendered
// independently (§4.2.2) — there is no state where one is fresh and the other stale.
func upsertTileRendered(ctx context.Context, pool *pgxpool.Pool, userID string, zoom, x, y int, fogKey, heatmapKey string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, heatmap_object_key, dirty, rendered_at)
		VALUES ($1, $2, $3, $4, $5, $6, false, NOW())
		ON CONFLICT (user_id, zoom, tile_x, tile_y)
		DO UPDATE SET object_key = EXCLUDED.object_key, heatmap_object_key = EXCLUDED.heatmap_object_key,
			dirty = false, rendered_at = NOW()
	`, userID, zoom, x, y, fogKey, heatmapKey)
	return err
}
