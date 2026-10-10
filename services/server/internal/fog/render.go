package fog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"math"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parallel"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// renderParallelism bounds how many tiles (or masks) a render works on at once. The work is
// mostly waiting — a z14 tile is a query for its masks and its two PUTs side by side, a
// pyramid tile a query, its children's GETs side by side and its two PUTs, each PUT or GET a
// round trip to R2 — so running tiles one after another left a render at about 1.3 tiles a
// second on the production droplet (the 2026-10-08 load test), almost none of it CPU. Bounded rather
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
//
// One account's passes never overlap (lockRenders): a `render_fog` job runs in a lane of its
// own (internal/worker), beside the account's edit or reprivacy job that renders inline.
func RenderUser(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string) (err error) {
	unlock, err := lockRenders(ctx, pool, userID)
	if err != nil {
		return fmt.Errorf("fog: lock renders: %w", err)
	}
	defer unlock()

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
	// Masks a z14 tile was composited without (renderAndStoreTile): logged once per pass, so
	// coverage missing after a restore isn't silent.
	var skipped atomic.Int64
	defer func() {
		if n := skipped.Load(); n > 0 {
			slog.Warn("fog: render skipped unreadable activity masks", "user", userID, "masks", n)
		}
	}()

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
				err = renderAndStoreTile(ctx, pool, store, userID, z, t[0], t[1], heatmapCap, &skipped)
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

// renderLockClass is the first key of the advisory lock lockRenders takes, so it can't meet
// any other advisory lock this database might take with the same user hash.
const renderLockClass = 0x666f67 // "fog"

// lockRenders waits for, then holds, a session advisory lock on one account's renders until
// the returned unlock is called. Two passes side by side would race over the same tile: the
// one that read the masks first can store its tile after the other's fresher one, and the
// fresher pass has already cleared the dirty flag (upsertTileRendered), so nothing would ever
// redraw it. A session lock rather than a transaction's, so a pass minutes long doesn't keep a
// transaction open; it holds one pool connection for as long.
func lockRenders(ctx context.Context, pool *pgxpool.Pool, userID string) (unlock func(), err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, hashtext($2))`, renderLockClass, userID); err != nil {
		// Cancelled mid-wait, the session may or may not hold the lock: drop it rather than
		// hand it back to the pool.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		conn.Release()
		return nil, err
	}
	return func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1, hashtext($2))`, renderLockClass, userID); err != nil {
			// Closing the session is what releases the lock now.
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
		conn.Release()
	}, nil
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
// The masks' bytes come in the same query, from activity_tile_mask_data (ADR-0040), so a tile
// costs one SQL read rather than a round trip to object storage per activity through it. A
// row from before that table, with an object key and no bytes yet, is still fetched from the
// store until `rerender-coverage --masks` redraws it. A mask that can't be read — neither
// bytes nor key (a restore leaves the bytes out until that same rerun), or a failed fetch or
// decode — is left out of the tile and counted in skipped, rather than failing it: every other
// activity through the tile is still real coverage, the same "one bad file doesn't abort the
// batch" principle §5.1 applies to bulk ingest.
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
func renderAndStoreTile(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int, heatmapCap float64, skipped *atomic.Int64) error {
	gen, err := tileDirtyGen(ctx, pool, userID, zoom, x, y)
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `
		SELECT d.png, m.mask_object_key, a.in_heatmap_window
		FROM activity_tile_masks m
		JOIN activities a ON a.id = m.activity_id
		LEFT JOIN activity_tile_mask_data d USING (activity_id, zoom, tile_x, tile_y)
		WHERE a.user_id = $1 AND NOT a.edit_pending
		  AND m.zoom = $2 AND m.tile_x = $3 AND m.tile_y = $4
	`, userID, zoom, x, y)
	if err != nil {
		return fmt.Errorf("query activity masks: %w", err)
	}
	type storedMask struct {
		png      []byte
		key      *string
		inWindow bool
	}
	var rowsOut []storedMask
	for rows.Next() {
		var row storedMask
		if err := rows.Scan(&row.png, &row.key, &row.inWindow); err != nil {
			rows.Close()
			return fmt.Errorf("scan activity mask: %w", err)
		}
		rowsOut = append(rowsOut, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Side by side: a tile many activities cross (a daily commute) has as many masks, and one
	// not yet moved into the database is a round trip to the store.
	loaded := make([]*image.Gray, len(rowsOut))
	if err := parallel.ForEach(ctx, len(rowsOut), renderParallelism, func(ctx context.Context, i int) error {
		var mask *image.Gray
		var err error
		switch row := rowsOut[i]; {
		case row.png != nil:
			mask, err = decodeGray(bytes.NewReader(row.png))
		case row.key != nil:
			mask, err = loadCrispMask(ctx, store, *row.key)
		default:
			err = errors.New("no stored mask")
		}
		if err != nil {
			skipped.Add(1)
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
	if err := storeTilePair(ctx, store, userID, zoom, x, y, fogMask, heatmapMask); err != nil {
		return err
	}
	return upsertTileRendered(ctx, pool, userID, zoom, x, y, gen,
		fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y))
}

// loadCrispMask fetches and decodes one activity's crisp (unblurred) mask stored as an object
// — a row written before activity_tile_mask_data, read back by renderAndStoreTile until
// `rerender-coverage --masks` moves it into the database.
func loadCrispMask(ctx context.Context, store *storage.Store, key string) (*image.Gray, error) {
	obj, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return decodeGray(obj)
}

// decodeGray decodes a PNG this package wrote. png.Decode hands back *image.Gray for those
// grayscale PNGs, but convert explicitly rather than assume the concrete type.
func decodeGray(r io.Reader) (*image.Gray, error) {
	img, err := png.Decode(r)
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

// RenderActivityMasks renders exactly one activity's own crisp (unblurred) stroke mask for
// each of its already-computed touched z14 tiles (internal/ingest's computeTouchedTiles), and
// upserts one activity_tile_masks row per tile with its PNG in activity_tile_mask_data, both
// in one transaction (ADR-0040). Idempotent: safe to re-run for the same activity (a
// redelivered ingest job). A redrawn row's object key is cleared; the object itself, from
// before the bytes moved into the database, goes with the activity's prefix
// (activity-masks/{id}/) or the one-time cleanup in docs/DEPLOY.md §6.
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
	pngs := make([][]byte, len(tiles))
	// Rendered side by side: a long track crosses hundreds of tiles.
	if err := parallel.ForEach(ctx, len(tiles), renderParallelism, func(ctx context.Context, i int) error {
		x, y := tiles[i][0], tiles[i][1]
		b, err := encodeTilePNG(renderActivityMask(projectToTile(points, x, y, Zoom)...))
		if err != nil {
			return fmt.Errorf("activity mask z%d/%d/%d: %w", Zoom, x, y, err)
		}
		xs[i], ys[i], pngs[i] = int32(x), int32(y), b
		return nil
	}); err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed
	if _, err := tx.Exec(ctx, `
		INSERT INTO activity_tile_masks (activity_id, zoom, tile_x, tile_y, mask_object_key, rendered_at)
		SELECT $1, $2, x, y, NULL, NOW()
		FROM unnest($3::int[], $4::int[]) AS t(x, y)
		ON CONFLICT (activity_id, zoom, tile_x, tile_y)
		DO UPDATE SET mask_object_key = NULL, rendered_at = NOW()
	`, activityID, Zoom, xs, ys); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO activity_tile_mask_data (activity_id, zoom, tile_x, tile_y, png)
		SELECT $1, $2, x, y, png
		FROM unnest($3::int[], $4::int[], $5::bytea[]) AS t(x, y, png)
		ON CONFLICT (activity_id, zoom, tile_x, tile_y)
		DO UPDATE SET png = EXCLUDED.png
	`, activityID, Zoom, xs, ys, pngs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveActivityMasks deletes one activity's masks for the given tiles — the tiles an edited
// track (internal/ingest's ProcessTrackEdit) no longer touches. Deleting the rows takes their
// bytes with them (the cascade); a row still pointing at an object from before
// activity_tile_mask_data has that object removed after. Rows go first: a render_fog pass
// running in between then simply composites without them.
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
	rows, err := pool.Query(ctx, `
		DELETE FROM activity_tile_masks m
		USING unnest($3::int[], $4::int[]) AS t(x, y)
		WHERE m.activity_id = $1 AND m.zoom = $2 AND m.tile_x = t.x AND m.tile_y = t.y
		RETURNING m.mask_object_key
	`, activityID, Zoom, xs, ys)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[*string])
	if err != nil {
		return err
	}
	var objects []string
	for _, k := range keys {
		if k != nil {
			objects = append(objects, *k)
		}
	}
	return parallel.ForEach(ctx, len(objects), renderParallelism, func(ctx context.Context, i int) error {
		if err := store.Remove(ctx, objects[i]); err != nil {
			return fmt.Errorf("remove activity mask %s: %w", objects[i], err)
		}
		return nil
	})
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
//
// The children's keys come in one query and their images are fetched side by side: one
// after another, a pyramid tile was about ten round trips to object storage.
func renderPyramidLevel(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, x, y int) error {
	gen, err := tileDirtyGen(ctx, pool, userID, zoom, x, y)
	if err != nil {
		return err
	}
	children, err := loadChildren(ctx, pool, store, userID, zoom+1, x, y)
	if err != nil {
		return err
	}
	fogMask := downsampleQuadrants([4]*image.Gray{children[0], children[1], children[2], children[3]})
	heatmapMask := downsampleQuadrants([4]*image.Gray{children[4], children[5], children[6], children[7]})
	if err := storeTilePair(ctx, store, userID, zoom, x, y, fogMask, heatmapMask); err != nil {
		return err
	}
	return upsertTileRendered(ctx, pool, userID, zoom, x, y, gen,
		fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y))
}

// loadChildren reads the four children at zoom of parent (px, py), in downsampleQuadrants'
// order (top left, top right, bottom left, bottom right): their Fog masks, then their Heatmap
// masks. A child with no fog_tiles row, or not rendered on one side, is a blank tile — the
// correct stand-in for "nothing here", not an error, since a user's history need not touch
// every sibling tile. A blank mask is all-zero either way; FogTile and HeatmapTile each
// interpret zero correctly for their own mode (fully fogged vs. fully transparent).
func loadChildren(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID string, zoom, px, py int) ([8]*image.Gray, error) {
	var out [8]*image.Gray
	for i := range out {
		out[i] = blankTile()
	}
	rows, err := pool.Query(ctx, `
		SELECT tile_x, tile_y, object_key, heatmap_object_key FROM fog_tiles
		WHERE user_id = $1 AND zoom = $2 AND tile_x = ANY($3) AND tile_y = ANY($4)`,
		userID, zoom, []int32{int32(px * 2), int32(px*2 + 1)}, []int32{int32(py * 2), int32(py*2 + 1)})
	if err != nil {
		return out, err
	}
	keys := make([]string, 8)
	for rows.Next() {
		var cx, cy int
		var fogKey, heatmapKey *string
		if err := rows.Scan(&cx, &cy, &fogKey, &heatmapKey); err != nil {
			rows.Close()
			return out, err
		}
		q := (cx - px*2) + 2*(cy-py*2)
		if fogKey != nil {
			keys[q] = *fogKey
		}
		if heatmapKey != nil {
			keys[4+q] = *heatmapKey
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	err = parallel.ForEach(ctx, len(keys), len(keys), func(ctx context.Context, i int) error {
		if keys[i] == "" {
			return nil
		}
		obj, err := store.Get(ctx, keys[i])
		if err != nil {
			return err
		}
		defer obj.Close()
		img, err := decodeGray(obj)
		if err != nil {
			return err
		}
		out[i] = img
		return nil
	})
	return out, err
}

// storeTilePair stores one tile's Fog and Heatmap masks, side by side.
func storeTilePair(ctx context.Context, store *storage.Store, userID string, zoom, x, y int, fogMask, heatmapMask *image.Gray) error {
	keys := [2]string{fogObjectKey(userID, zoom, x, y), heatmapObjectKey(userID, zoom, x, y)}
	masks := [2]*image.Gray{fogMask, heatmapMask}
	return parallel.ForEach(ctx, 2, 2, func(ctx context.Context, i int) error {
		return storeTilePNG(ctx, store, keys[i], masks[i])
	})
}

func storeTilePNG(ctx context.Context, store *storage.Store, key string, mask *image.Gray) error {
	b, err := encodeTilePNG(mask)
	if err != nil {
		return err
	}
	return store.Put(ctx, key, bytes.NewReader(b), int64(len(b)))
}

func encodeTilePNG(mask *image.Gray) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, mask); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
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
