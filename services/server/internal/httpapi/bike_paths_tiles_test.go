package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// The bike-path tiles (§4.24) carry each way with its kind and name from zoom 9 up, and leave a
// way shorter than a pixel out until zoom 13.
func TestBikePathsTile(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)

	at := [2]float64{-40.5, -22.5} // lon, lat: open ocean, clear of every other test's fixtures
	stamp := time.Now().UnixNano()
	long, short := fmt.Sprintf("Test Greenway %d", stamp), fmt.Sprintf("Test Stub %d", stamp)
	insert := func(kind, name string, lengthDeg float64, osmID int64) {
		t.Helper()
		if _, err := d.pool.Exec(ctx, `
			INSERT INTO bike_paths (kind, name, geom, osm_id, last_seen_import)
			VALUES ($1, $2, ST_Multi(ST_Transform(ST_SetSRID(ST_MakeLine(ST_MakePoint($3, $4), ST_MakePoint($3 + $5, $4)), 4326), 3857)), $6, now())
		`, kind, name, at[0], at[1], lengthDeg, osmID); err != nil {
			t.Fatalf("create bike path: %v", err)
		}
		t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM bike_paths WHERE osm_id = $1`, osmID) })
	}
	insert("cycleway", long, 0.05, stamp)    // ~5 km
	insert("shared", short, 0.0001, stamp+1) // ~10 m: under a pixel below zoom 13

	tile := func(z int) []byte {
		t.Helper()
		x, y := tilemath.LonLatToTile(at[0], at[1], z)
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/bike-paths/%d/%d/%d.mvt", z, x, y), nil)
		d.decode(rec, http.StatusOK, nil)
		return rec.Body.Bytes()
	}

	z9 := tile(9)
	for _, want := range []string{"bike_paths", long, "cycleway"} {
		if !bytes.Contains(z9, []byte(want)) {
			t.Errorf("the z9 tile doesn't carry %q", want)
		}
	}
	if bytes.Contains(z9, []byte(short)) {
		t.Errorf("the z9 tile carries a way shorter than a pixel")
	}
	z13 := tile(13)
	for _, want := range []string{long, short, "shared"} {
		if !bytes.Contains(z13, []byte(want)) {
			t.Errorf("the z13 tile doesn't carry %q", want)
		}
	}
	if n := len(tile(8)); n != 0 {
		t.Errorf("the z8 tile has %d bytes; bike paths start at z9", n)
	}

	if rec := d.do(account{}, "GET", "/tiles/v1/bike-paths/9/0/0.mvt", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}
}
