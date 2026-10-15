package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
)

// A Fog vector tile is traced from the account's stored coverage tile: one with a road across
// it comes back traced from it; one with nothing on file, as all veil.
func TestFogVectorTile(t *testing.T) {
	mem := newMemS3()
	d := newDBTestWithS3(t, mem)
	ctx := context.Background()
	me := d.newAccount(false)

	cov := image.NewGray(image.Rect(0, 0, 128, 128))
	for y := 60; y < 68; y++ {
		for x := range 128 {
			cov.Pix[y*128+x] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, cov); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("fog/%s/14/4471/6119.png", me.id)
	mem.Put(key, buf.Bytes())
	if _, err := d.pool.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, dirty) VALUES ($1, 14, 4471, 6119, $2, false)`,
		me.id, key); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		x    int
		want *image.Gray
	}{{4471, cov}, {4472, image.NewGray(image.Rect(0, 0, 128, 128))}} {
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/fog-vector/14/%d/6119.mvt", c.x), nil)
		if rec.Code != 200 {
			t.Fatalf("x %d: status %d", c.x, rec.Code)
		}
		if !bytes.Equal(rec.Body.Bytes(), fog.EncodeFogTile(c.want, false)) {
			t.Errorf("x %d: not the tile traced from what's on file", c.x)
		}
	}
}
