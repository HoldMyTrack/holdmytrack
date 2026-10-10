package fog

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

// Reads TEST_DATABASE_URL and skips without it, like internal/httpapi's database tests
// (docs/DEVELOPMENT.md).
func testAccount(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	var userID string
	email := fmt.Sprintf("fog-test-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete account: %v", err)
		}
	})
	return pool, userID
}

func tileDirty(t *testing.T, pool *pgxpool.Pool, userID string, zoom, x, y int) bool {
	t.Helper()
	var dirty bool
	if err := pool.QueryRow(context.Background(),
		`SELECT dirty FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, zoom, x, y).Scan(&dirty); err != nil {
		t.Fatalf("read z%d/%d/%d: %v", zoom, x, y, err)
	}
	return dirty
}

// A render clears the tile's dirty flag only if nothing marked it again while it was drawn —
// an activity deleted mid-render must not leave its old mask in the stored tile for good —
// and always leaves its parent dirty, so the pyramid above survives an interrupted pass.
func TestUpsertTileRenderedKeepsAMidRenderMark(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	const z, x, y = Zoom, 100, 201
	if _, err := pool.Exec(ctx,
		`INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty) VALUES ($1, $2, $3, $4, true)`,
		userID, z, x, y); err != nil {
		t.Fatal(err)
	}

	gen, err := tileDirtyGen(ctx, pool, userID, z, x, y)
	if err != nil {
		t.Fatal(err)
	}
	// What MarkFogTilesDirty does when a delete lands while the render is compositing.
	if _, err := pool.Exec(ctx,
		`UPDATE fog_tiles SET dirty = true, dirty_gen = dirty_gen + 1 WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, z, x, y); err != nil {
		t.Fatal(err)
	}
	if err := upsertTileRendered(ctx, pool, userID, z, x, y, gen, "fog", "heat"); err != nil {
		t.Fatal(err)
	}
	if !tileDirty(t, pool, userID, z, x, y) {
		t.Error("tile marked mid-render was cleared")
	}
	if !tileDirty(t, pool, userID, z-1, x/2, y/2) {
		t.Error("parent not marked dirty")
	}

	gen, err = tileDirtyGen(ctx, pool, userID, z, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertTileRendered(ctx, pool, userID, z, x, y, gen, "fog", "heat"); err != nil {
		t.Fatal(err)
	}
	if tileDirty(t, pool, userID, z, x, y) {
		t.Error("tile rendered with nothing marking it since stayed dirty")
	}
}

func memStore(t *testing.T) *storage.Store {
	t.Helper()
	srv := httptest.NewServer(storagetest.New())
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func tileRendered(t *testing.T, pool *pgxpool.Pool, userID string, zoom, x, y int) bool {
	t.Helper()
	var key *string
	var dirty bool
	err := pool.QueryRow(context.Background(),
		`SELECT object_key, dirty FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, zoom, x, y).Scan(&key, &dirty)
	return err == nil && key != nil && !dirty
}

// A dirty z14 tile is rendered, then every ancestor up to z0.
func TestRenderUserBuildsThePyramid(t *testing.T) {
	pool, userID := testAccount(t)
	const x, y = 8800, 5400
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty) VALUES ($1, $2, $3, $4, true)`,
		userID, Zoom, x, y); err != nil {
		t.Fatal(err)
	}
	if err := RenderUser(context.Background(), pool, memStore(t), userID); err != nil {
		t.Fatal(err)
	}
	for z := Zoom; z >= 0; z-- {
		shift := Zoom - z
		if !tileRendered(t, pool, userID, z, x>>shift, y>>shift) {
			t.Errorf("z%d ancestor not rendered", z)
		}
	}
}

// A pass cut off after its z14 render, before the levels above, left their tiles stale with
// nothing marking them; the next pass finishes them now. Here, the state such a pass leaves:
// z14 rendered and clean, its parent dirty.
func TestRenderUserFinishesAnInterruptedPyramid(t *testing.T) {
	pool, userID := testAccount(t)
	const x, y = 8800, 5400
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, heatmap_object_key, dirty, rendered_at)
		VALUES ($1, $2, $3, $4, NULL, NULL, false, NOW()), ($1, $2 - 1, $3 / 2, $4 / 2, NULL, NULL, true, NULL)
	`, userID, Zoom, x, y); err != nil {
		t.Fatal(err)
	}
	if err := RenderUser(context.Background(), pool, memStore(t), userID); err != nil {
		t.Fatal(err)
	}
	for z := Zoom - 1; z >= 0; z-- {
		shift := Zoom - z
		if !tileRendered(t, pool, userID, z, x>>shift, y>>shift) {
			t.Errorf("z%d not rendered", z)
		}
	}
}

// One account's render passes run one at a time: a pass started while another holds the
// account's lock waits for it, while another account's pass goes ahead.
func TestRenderUserWaitsForTheAccountsRunningPass(t *testing.T) {
	pool, userID := testAccount(t)
	_, otherID := testAccount(t)
	ctx := context.Background()

	unlock, err := lockRenders(ctx, pool, userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenderUser(ctx, pool, nil, otherID); err != nil {
		t.Fatalf("another account's render: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- RenderUser(ctx, pool, nil, userID) }()
	select {
	case err := <-done:
		t.Fatalf("render finished (err %v) while the account's other pass held the lock", err)
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("render after unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render still waiting after the other pass unlocked")
	}
}

// A pyramid tile is the downsample of its four children as stored, read in the right
// quadrants: one child with both rasters, one with both again, one with only Fog, one never
// rendered.
func TestRenderPyramidLevelReadsEachChild(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	mem := storagetest.New()
	srv := httptest.NewServer(mem)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	const z, px, py = Zoom - 1, 4400, 2700

	// Each child lit in a different column, so a child read into the wrong quadrant shows.
	child := func(col int, v uint8) *image.Gray {
		m := blankTile()
		for y := range TileSize {
			m.Pix[y*m.Stride+col] = v
		}
		return m
	}
	var fog, heat [4]*image.Gray
	for q := range 4 {
		fog[q], heat[q] = blankTile(), blankTile()
	}
	put := func(key string, m *image.Gray) {
		b, err := encodeTilePNG(m)
		if err != nil {
			t.Fatal(err)
		}
		mem.Put(key, b)
	}
	for q, c := range [][2]int{{px * 2, py * 2}, {px*2 + 1, py * 2}, {px * 2, py*2 + 1}} {
		fog[q] = child(10+q*20, 255)
		fogKey := fogObjectKey(userID, z+1, c[0], c[1])
		put(fogKey, fog[q])
		var heatKey *string
		if q < 2 {
			heat[q] = child(15+q*20, 128)
			k := heatmapObjectKey(userID, z+1, c[0], c[1])
			put(k, heat[q])
			heatKey = &k
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, heatmap_object_key, dirty, rendered_at)
			VALUES ($1, $2, $3, $4, $5, $6, false, NOW())`, userID, z+1, c[0], c[1], fogKey, heatKey); err != nil {
			t.Fatal(err)
		}
	}

	if err := renderPyramidLevel(ctx, pool, store, userID, z, px, py); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		key  string
		want *image.Gray
	}{
		{fogObjectKey(userID, z, px, py), downsampleQuadrants(fog)},
		{heatmapObjectKey(userID, z, px, py), downsampleQuadrants(heat)},
	} {
		b, ok := mem.Object(c.key)
		if !ok {
			t.Fatalf("%s not stored", c.key)
		}
		got, err := decodeGray(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Pix, c.want.Pix) {
			t.Errorf("%s isn't the downsample of its children as stored", c.key)
		}
	}
}
