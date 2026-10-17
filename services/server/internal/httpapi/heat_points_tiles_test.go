package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/HoldMyTrack/platform/services/server/internal/fog"
)

// A heat-points tile is made from the account's stored count tile one zoom up: points where it
// has passes, an empty tile where it has none, past the last zoom drawn, or where the stored
// tile is still the 8-bit raster from before the counts.
func TestHeatPointsTile(t *testing.T) {
	mem := newMemS3()
	d := newDBTestWithS3(t, mem)
	ctx := context.Background()
	me := d.newAccount(false)

	encode := func(m image.Image) []byte {
		var buf bytes.Buffer
		if err := png.Encode(&buf, m); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	store := func(x, y int, b []byte) {
		key := fmt.Sprintf("heatmap/%s/14/%d/%d.png", me.id, x, y)
		mem.Put(key, b)
		if _, err := d.pool.Exec(ctx, `
			INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, heatmap_object_key, dirty)
			VALUES ($1, 14, $2, $3, $4, false)`, me.id, x, y, key); err != nil {
			t.Fatal(err)
		}
	}
	counts := image.NewGray16(image.Rect(0, 0, 128, 128))
	counts.SetGray16(10, 20, color.Gray16{Y: 3}) // the top-left quarter
	store(4471, 6119, encode(counts))
	old := image.NewGray(image.Rect(0, 0, 128, 128))
	old.Pix[0] = 200
	store(4472, 6119, encode(old))

	for _, c := range []struct {
		z, x, y int
		empty   bool
		why     string
	}{
		{15, 8942, 12238, false, "the quarter with passes"},
		{15, 8943, 12238, true, "the quarter without"},
		{15, 8944, 12238, true, "a tile still in the old raster"},
		{16, 17884, 24476, true, "past the last zoom drawn"},
	} {
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/heat-points/%d/%d/%d.mvt", c.z, c.x, c.y), nil)
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", c.why, rec.Code)
		}
		body := rec.Body.Bytes()
		if c.empty && len(body) != 0 {
			t.Errorf("%s: %d bytes, want an empty tile", c.why, len(body))
		}
		if !c.empty && !bytes.Contains(body, []byte("heat")) {
			t.Errorf("%s: no heat layer in %d bytes", c.why, len(body))
		}
	}

	// Zoomed out, points come from the activities' mask rows, not the count tiles: an activity
	// through z14 tile (4471, 6119) puts a point in the z5 tile over it and none beside it, and
	// a render stores each zoom's busiest cell.
	for range 3 {
		id := d.newActivity(me, testActivity{activityType: "walking"})
		if _, err := d.pool.Exec(ctx,
			`INSERT INTO activity_tile_masks (activity_id, zoom, tile_x, tile_y) VALUES ($1, 14, 4471, 6119)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := fog.StoreHeatPointCaps(ctx, d.pool, me.id); err != nil {
		t.Fatal(err)
	}
	var caps []int
	if err := d.pool.QueryRow(ctx, `SELECT heat_point_caps FROM users WHERE id = $1`, me.id).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	if len(caps) != fog.HeatActivityMaxZoom+1 || caps[5] != 3 {
		t.Errorf("heat point caps %v, want %d zooms, 3 activities in the busiest cell", caps, fog.HeatActivityMaxZoom+1)
	}
	for _, c := range []struct {
		x, y  int
		empty bool
	}{{4471 >> 9, 6119 >> 9, false}, {4471>>9 + 1, 6119 >> 9, true}} {
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/heat-points/5/%d/%d.mvt", c.x, c.y), nil)
		if rec.Code != 200 {
			t.Fatalf("z5 %d/%d: status %d", c.x, c.y, rec.Code)
		}
		if got := rec.Body.Len() == 0; got != c.empty {
			t.Errorf("z5 %d/%d: %d bytes, want empty %v", c.x, c.y, rec.Body.Len(), c.empty)
		}
	}
}
