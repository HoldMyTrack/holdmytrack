package fog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parallel"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// renderParallelism bounds how many tiles (or masks) a render works on at once. The work is
// mostly waiting on object storage — a tile is one or more GETs and two PUTs, each a round
// trip to R2 — so running them one after another left a render at about 1.3 tiles a second
// on the production droplet (the 2026-10-08 load test), almost none of it CPU. Bounded rather
// than unbounded so memory stays predictable (IMPLEMENTATION.md §5.2): each in-flight tile
// holds a few decoded masks.
const renderParallelism = 16

// RenderUser is the `render_fog` job body: re-render every tile currently marked dirty for
// this user, z14 first and then each pyramid level up to z0. Idempotent and complete per
// tile, not incremental — each render gathers *every* activity intersecting a tile, not just
// whichever one triggered the dirty flag, because a tile's mask has to represent the user's
// entire history through it every time, not just the newest activity.
//
// Rendering a tile marks its parent dirty, so the pyramid's pending work is in the database,
// not in memory: a pass cut off partway (a storage error, a deploy) leaves the parents of
// what it rendered dirty, and the next pass finishes them rather than leaving z13 and above
// stale until something else happens to touch the same tiles.
//
// A pass that stored anything ends by bumping map_version (BumpMapVersion), a failed one
// too — its tiles are already overwritten — so a client never keeps a cached tile past it.
func RenderUser(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string) (err error) {
	rendered := false
	defer func() {
		if !rendered {
			return
		}
		// On a context a shutdown hasn't cancelled: the tiles stored before it are new either way.
		if berr := BumpMapVersion(context.WithoutCancel(ctx), pool, userID); berr != nil && err == nil {
			err = fmt.Errorf("fog: bump map version: %w", berr)
		}
	}()

	heatmapCap, err := userHeatmapCap(ctx, pool, userID)
	if err != nil {
		return fmt.Errorf("fog: load heatmap cap: %w", err)
	}

	// Above z14 the client overzooms the z14 raster directly (§4.2) — no pyramid needed there.
	for z := Zoom; z >= 0; z-- {
		dirty, err := dirtyTiles(ctx, pool, userID, z)
		if err != nil {
			return fmt.Errorf("fog: list dirty z%d tiles: %w", z, err)
		}
		if len(dirty) > 0 {
			rendered = true
		}
		// A level's tiles are independent of each other, so they render side by side; the
		// next level up waits for all of them, since its tiles are built from these.
		err = parallel.ForEach(ctx, len(dirty), renderParallelism, func(ctx context.Context, i int) error {
			t := dirty[i]
			var err error
			if z == Zoom {
				err = renderAndStoreTile(ctx, pool, store, userID, z, t[0], t[1], heatmapCap)
			} else {
				err = renderPyramidLevel(ctx, pool, store, userID, z, t[0], t[1])
			}
			if err != nil {
				return fmt.Errorf("fog: render z%d/%d/%d: %w", z, t[0], t[1], err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Private locations are never re-applied here: RenderActivityMasks renders from points
// already clipped at ingest, and renderAndStoreTile composites those masks rather than
// re-parsing raw payloads. A location change goes through the `reprivacy` job instead, which
// re-renders each affected activity's own masks from freshly clipped points (internal/ingest's
// ProcessReprivacy) before this recomposites.

// userHeatmapCap reads the account's own users.heatmap_cap (migrations/0001_users_and_auth.sql)
// — kept current by internal/worker/heatmap_cap.go's daily sweep (cap.go's
// RecomputeHeatmapCap), never derived here. A render pass always uses whatever value is
// currently stored, even if a recompute is mid-flight elsewhere; the next render after that
// sweep's own dirty-marking picks up the new value, the same eventual-consistency the rest of
// this package's dirty-tile model already relies on.
func userHeatmapCap(ctx context.Context, pool *pgxpool.Pool, userID string) (float64, error) {
	var cap float64
	err := pool.QueryRow(ctx, `SELECT heatmap_cap FROM users WHERE id = $1`, userID).Scan(&cap)
	return cap, err
}

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
// aggregate (every activity's mask), while Heatmap only composites masks whose
// activity is currently flagged `in_heatmap_window` — a plain column read, not a comparison
// against "now" here. Keeping that flag current as activities age past HeatmapWindowDays is
// internal/worker's job (heatmap_aging.go's daily sweep), not this function's — by the time an
// activity's flag actually flips, that sweep has already marked its tiles dirty too, so this
// render is always just reflecting whatever the flag already says, never deciding it itself.
//
// Neither includes a Pending activity (edit_pending, §4.7.7): its masks are the pre-reprocess
// ones, or mid-rewrite. The reprocess marks its tiles dirty when it goes Pending and renders
// them again after clearing the flag, so it drops out of both rasters for the duration.
func renderAndStoreTile(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int, heatmapCap float64) error {
	gen, err := tileDirtyGen(ctx, pool, userID, zoom, x, y)
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `
		SELECT m.mask_object_key, a.in_heatmap_window
		FROM activity_tile_masks m
		JOIN activities a ON a.id = m.activity_id
		WHERE a.user_id = $1 AND NOT a.edit_pending
		  AND m.zoom = $2 AND m.tile_x = $3 AND m.tile_y = $4
	`, userID, zoom, x, y)
	if err != nil {
		return fmt.Errorf("query activity masks: %w", err)
	}
	type keyedMask struct {
		key      string
		inWindow bool
	}
	var rowsOut []keyedMask
	for rows.Next() {
		var row keyedMask
		if err := rows.Scan(&row.key, &row.inWindow); err != nil {
			rows.Close()
			return fmt.Errorf("scan activity mask key: %w", err)
		}
		rowsOut = append(rowsOut, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Fetched side by side: a tile many activities cross (a daily commute) has as many masks.
	loaded := make([]*image.Gray, len(rowsOut))
	if err := parallel.ForEach(ctx, len(rowsOut), renderParallelism, func(ctx context.Context, i int) error {
		mask, err := loadCrispMask(ctx, store, rowsOut[i].key)
		if err != nil {
			// One missing/corrupt mask should not fail the whole tile — every other
			// activity through it is still real coverage. Skip and continue, the same
			// "one bad file doesn't abort the batch" principle §5.1 applies to bulk
			// ingest.
			return nil
		}
		loaded[i] = mask
		return nil
	}); err != nil {
		return err
	}
	fogMasks := make([]*image.Gray, 0, len(rowsOut))
	heatmapMasks := make([]*image.Gray, 0, len(rowsOut))
	for i, row := range rowsOut {
		mask := loaded[i]
		if mask == nil {
			continue
		}
		fogMasks = append(fogMasks, mask)
		if row.inWindow {
			heatmapMasks = append(heatmapMasks, mask)
		}
	}

	// Both rasters composite the same shape of input differently (§4.2.2: max vs sum) — see
	// compositeFogMask/compositeHeatmapMask in raster.go — but, per the window above, not
	// necessarily the same *set* of masks.
	fogMask := compositeFogMask(fogMasks)
	heatmapMask := compositeHeatmapMask(heatmapMasks, heatmapCap)
	if err := storeTilePNG(ctx, store, fogObjectKey(userID, zoom, x, y), fogMask); err != nil {
		return err
	}
	if err := storeTilePNG(ctx, store, heatmapObjectKey(userID, zoom, x, y), heatmapMask); err != nil {
		return err
	}
	return upsertTileRendered(ctx, pool, userID, zoom, x, y, gen,
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
// privacy-clipped) — unlike the old per-tile re-render this replaces, this never reads raw
// payloads back from object storage for the activity that triggered it. Ingest-time
// rendering cost is now proportional to just this one activity's own tiles, independent of
// how many *other* activities already touch them — the old design re-parsed every one of
// them on every tile they shared.
func RenderActivityMasks(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, activityID string, points []parse.Point, tiles [][2]int) error {
	if len(tiles) == 0 {
		return nil
	}

	xs := make([]int32, len(tiles))
	ys := make([]int32, len(tiles))
	keys := make([]string, len(tiles))
	// Rendered and stored side by side: a long track crosses hundreds of tiles, and storing
	// their masks one after another cost a 1,500 km drive over three minutes of the worker.
	if err := parallel.ForEach(ctx, len(tiles), renderParallelism, func(ctx context.Context, i int) error {
		x, y := tiles[i][0], tiles[i][1]
		mask := renderActivityMask(projectToTile(points, x, y, Zoom)...)
		key := activityMaskObjectKey(activityID, Zoom, x, y)
		if err := storeTilePNG(ctx, store, key, mask); err != nil {
			return fmt.Errorf("store activity mask z%d/%d/%d: %w", Zoom, x, y, err)
		}
		xs[i], ys[i], keys[i] = int32(x), int32(y), key
		return nil
	}); err != nil {
		return err
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

// RemoveActivityMasks deletes one activity's masks for the given tiles, both the
// activity_tile_masks rows and their objects — the tiles an edited track (internal/ingest's
// ProcessTrackEdit) no longer touches. Rows go first: a render_fog pass running in between
// then simply composites without them, rather than failing on a row whose object is gone.
func RemoveActivityMasks(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, activityID string, tiles [][2]int) error {
	if len(tiles) == 0 {
		return nil
	}
	xs := make([]int32, 0, len(tiles))
	ys := make([]int32, 0, len(tiles))
	for _, t := range tiles {
		xs = append(xs, int32(t[0]))
		ys = append(ys, int32(t[1]))
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM activity_tile_masks m
		USING unnest($3::int[], $4::int[]) AS t(x, y)
		WHERE m.activity_id = $1 AND m.zoom = $2 AND m.tile_x = t.x AND m.tile_y = t.y
	`, activityID, Zoom, xs, ys); err != nil {
		return err
	}
	return parallel.ForEach(ctx, len(tiles), renderParallelism, func(ctx context.Context, i int) error {
		t := tiles[i]
		if err := store.Remove(ctx, activityMaskObjectKey(activityID, Zoom, t[0], t[1])); err != nil {
			return fmt.Errorf("remove activity mask z%d/%d/%d: %w", Zoom, t[0], t[1], err)
		}
		return nil
	})
}

func activityMaskObjectKey(activityID string, zoom, x, y int) string {
	return fmt.Sprintf("activity-masks/%s/%d/%d/%d.png", activityID, zoom, x, y)
}

// projectToTile places points in the tile's own pixels, once for each copy of the world that
// reaches it: a track continuing past ±180 (ingest's unwrapLons) lies partly in the next copy
// over, whose pixels are a world's width away, so a tile on the other side of the
// antimeridian is drawn from the track shifted by that width. A copy that doesn't come within
// TileMarginPx of the tile is left out: the rasterizer still pays for a path a world away
// from its canvas, over a minute at z14.
func projectToTile(points []parse.Point, tileX, tileY, zoom int) [][]pixelPoint {
	world := math.Pow(2, float64(zoom)) * TileSize
	originX := float64(tileX) * TileSize
	originY := float64(tileY) * TileSize
	base := make([]pixelPoint, len(points))
	minX, maxX := math.Inf(1), math.Inf(-1)
	for i, p := range points {
		wx, wy := tilemath.WorldPixel(p.Lon, p.Lat, zoom, TileSize)
		base[i] = pixelPoint{x: wx - originX, y: wy - originY}
		minX, maxX = math.Min(minX, base[i].x), math.Max(maxX, base[i].x)
	}
	var out [][]pixelPoint
	for _, shift := range []float64{0, -world, world} {
		if maxX+shift < -TileMarginPx || minX+shift > TileSize+TileMarginPx {
			continue
		}
		if shift == 0 {
			out = append(out, base)
			continue
		}
		shifted := make([]pixelPoint, len(base))
		for i, p := range base {
			shifted[i] = pixelPoint{x: p.x + shift, y: p.y}
		}
		out = append(out, shifted)
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
	gen, err := tileDirtyGen(ctx, pool, userID, zoom, x, y)
	if err != nil {
		return err
	}
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
	return upsertTileRendered(ctx, pool, userID, zoom, x, y, gen,
		fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y))
}

// loadTileImage fetches a tile's currently-stored mask (fog or heatmap, per column) or a
// blank one if the tile has no fog_tiles row yet or hasn't been rendered on that side — the
// correct stand-in for "nothing here", not an error, since a user's history need not touch
// every sibling tile. A blank mask is all-zero either way; FogTile and HeatmapTile
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

// tileDirtyGen is a tile's dirty_gen as a render starts, read before anything it draws from:
// upsertTileRendered clears dirty only if no one has marked the tile again since. 0 for a
// tile with no row yet.
func tileDirtyGen(ctx context.Context, pool *pgxpool.Pool, userID string, zoom, x, y int) (int64, error) {
	var gen int64
	err := pool.QueryRow(ctx,
		`SELECT dirty_gen FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, zoom, x, y,
	).Scan(&gen)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return gen, err
}

// upsertTileRendered records a fresh render of *both* rasters in one write, and marks the
// tile's parent dirty in the same transaction (RenderUser walks the pyramid off that flag).
// INSERT ... ON CONFLICT, not a plain UPDATE: a pyramid tile (z13..z0) may have no fog_tiles
// row yet the first time it's built. One write, not two, because the two rasters are never
// rendered independently (§4.2.2) — there is no state where one is fresh and the other stale.
//
// dirty clears only if dirty_gen is still gen, the value read before the render gathered its
// masks. A change that lands mid-render — an activity deleted while its old mask was being
// composited in — marks the tile again (bumping dirty_gen), and the tile stays dirty for the
// render the change enqueued, instead of keeping the deleted track until something else
// touches that tile.
func upsertTileRendered(ctx context.Context, pool *pgxpool.Pool, userID string, zoom, x, y int, gen int64, fogKey, heatmapKey string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if _, err := tx.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, heatmap_object_key, dirty, rendered_at)
		VALUES ($1, $2, $3, $4, $5, $6, false, NOW())
		ON CONFLICT (user_id, zoom, tile_x, tile_y)
		DO UPDATE SET object_key = EXCLUDED.object_key, heatmap_object_key = EXCLUDED.heatmap_object_key,
			dirty = fog_tiles.dirty_gen <> $7, rendered_at = NOW()
	`, userID, zoom, x, y, fogKey, heatmapKey, gen); err != nil {
		return err
	}
	if zoom > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (user_id, zoom, tile_x, tile_y) DO UPDATE SET dirty = true, dirty_gen = fog_tiles.dirty_gen + 1
		`, userID, zoom-1, x/2, y/2); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
