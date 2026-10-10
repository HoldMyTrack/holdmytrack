package fog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// maskStore is memStore that also counts the GETs it answers.
func maskStore(t *testing.T) (*storage.Store, *storagetest.MemS3, *atomic.Int64) {
	t.Helper()
	mem := storagetest.New()
	var gets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, lookup := r.URL.Query()["location"]; r.Method == http.MethodGet && !lookup {
			gets.Add(1)
		}
		mem.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	return store, mem, &gets
}

func insertActivity(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO activities (user_id, source, activity_type, started_at, timezone)
		VALUES ($1, 'upload', 'walking', NOW(), 'UTC') RETURNING id`, userID).Scan(&id); err != nil {
		t.Fatalf("create activity: %v", err)
	}
	return id
}

// A short walk inside one z14 tile, and that tile.
func walkInOneTile() ([]parse.Point, [2]int) {
	const lon, lat = 13.40, 52.52
	x, y := tilemath.LonLatToTile(lon, lat, Zoom)
	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	pts := []parse.Point{
		{Lat: lat, Lon: lon, Time: start},
		{Lat: lat + 0.001, Lon: lon + 0.001, Time: start.Add(time.Minute)},
	}
	return pts, [2]int{x, y}
}

func markDirty(t *testing.T, pool *pgxpool.Pool, userID string, tile [2]int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty) VALUES ($1, $2, $3, $4, true)`,
		userID, Zoom, tile[0], tile[1]); err != nil {
		t.Fatal(err)
	}
}

// fogTileLit reports whether the stored z14 Fog tile has any revealed pixel.
func fogTileLit(t *testing.T, mem *storagetest.MemS3, userID string, tile [2]int) bool {
	t.Helper()
	b, ok := mem.Object(fogObjectKey(userID, Zoom, tile[0], tile[1]))
	if !ok {
		t.Fatal("fog tile not stored")
	}
	img, err := decodeGray(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range img.Pix {
		if p != 0 {
			return true
		}
	}
	return false
}

// A mask is stored in the database, not the store, and a z14 render reads it back from there
// without a single GET.
func TestRenderedMaskIsReadBackWithoutTheStore(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	store, mem, gets := maskStore(t)
	activityID := insertActivity(t, pool, userID)
	pts, tile := walkInOneTile()

	if err := RenderActivityMasks(ctx, pool, activityID, pts, [][2]int{tile}); err != nil {
		t.Fatal(err)
	}
	var size int
	if err := pool.QueryRow(ctx,
		`SELECT length(png) FROM activity_tile_mask_data WHERE activity_id = $1`, activityID).Scan(&size); err != nil || size == 0 {
		t.Fatalf("mask PNG: %d bytes, %v; want it in the database", size, err)
	}

	markDirty(t, pool, userID, tile)
	var skipped atomic.Int64
	if err := renderAndStoreTile(ctx, pool, store, userID, Zoom, tile[0], tile[1], 1, &skipped); err != nil {
		t.Fatal(err)
	}
	if n := gets.Load(); n != 0 {
		t.Errorf("render made %d GETs, want none", n)
	}
	if !fogTileLit(t, mem, userID, tile) {
		t.Error("fog tile has nothing revealed")
	}
}

// A mask row with no PNG (a restore leaves them out until rerender-coverage --masks) is left
// out of the tile and counted; the activities beside it still show.
func TestRenderCountsMasksWithoutPNG(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	store, mem, _ := maskStore(t)
	pts, tile := walkInOneTile()

	drawn := insertActivity(t, pool, userID)
	if err := RenderActivityMasks(ctx, pool, drawn, pts, [][2]int{tile}); err != nil {
		t.Fatal(err)
	}
	missing := insertActivity(t, pool, userID)
	if _, err := pool.Exec(ctx, `INSERT INTO activity_tile_masks (activity_id, zoom, tile_x, tile_y) VALUES ($1, $2, $3, $4)`,
		missing, Zoom, tile[0], tile[1]); err != nil {
		t.Fatal(err)
	}
	markDirty(t, pool, userID, tile)

	var skipped atomic.Int64
	if err := renderAndStoreTile(ctx, pool, store, userID, Zoom, tile[0], tile[1], 1, &skipped); err != nil {
		t.Fatal(err)
	}
	if n := skipped.Load(); n != 1 {
		t.Errorf("skipped %d masks, want the one without a PNG", n)
	}
	if !fogTileLit(t, mem, userID, tile) {
		t.Error("fog tile left out the mask that has its PNG")
	}
}

// A mask's bytes go with it: removing an edited track's mask, or deleting the activity.
func TestMaskBytesGoWithTheirRow(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	pts, tile := walkInOneTile()
	neighbour := [2]int{tile[0] + 1, tile[1]}
	count := func(activityID string) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_tile_mask_data WHERE activity_id = $1`, activityID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	edited := insertActivity(t, pool, userID)
	if err := RenderActivityMasks(ctx, pool, edited, pts, [][2]int{tile, neighbour}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveActivityMasks(ctx, pool, edited, [][2]int{neighbour}); err != nil {
		t.Fatal(err)
	}
	if n := count(edited); n != 1 {
		t.Errorf("after removing one of two masks, %d PNGs left, want 1", n)
	}

	deleted := insertActivity(t, pool, userID)
	if err := RenderActivityMasks(ctx, pool, deleted, pts, [][2]int{tile}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM activities WHERE id = $1`, deleted); err != nil {
		t.Fatal(err)
	}
	if n := count(deleted); n != 0 {
		t.Errorf("deleted activity left %d mask PNGs", n)
	}
}
