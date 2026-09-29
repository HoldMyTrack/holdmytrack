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

// The spots tiles (§4.25) carry the places and their areas from zoom 12 up, with OSM's text.
func TestSpotsTile(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)

	at := [2]float64{-40.5, -20.5} // lon, lat: open ocean, clear of every other test's fixtures
	name := fmt.Sprintf("Test Monument %d", time.Now().UnixNano())
	var spotID int64
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO spots (category, name, inscription, wikipedia, geom, osm_type, osm_id)
		VALUES ('monument', $1, 'To the fallen', 'en:Test monument',
		        ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 30)::geometry), 'node', $4)
		RETURNING id
	`, name, at[0], at[1], time.Now().UnixNano()).Scan(&spotID); err != nil {
		t.Fatalf("create spot: %v", err)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, spotID) })

	tile := func(z int) []byte {
		t.Helper()
		x, y := tilemath.LonLatToTile(at[0], at[1], z)
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/spots/%d/%d/%d.mvt", z, x, y), nil)
		d.decode(rec, http.StatusOK, nil)
		return rec.Body.Bytes()
	}

	z14 := tile(14)
	for _, want := range []string{name, "To the fallen", "en:Test monument", "spot_areas", "circle"} {
		if !bytes.Contains(z14, []byte(want)) {
			t.Errorf("the z14 tile doesn't carry %q", want)
		}
	}
	if !bytes.Contains(tile(12), []byte(name)) {
		t.Errorf("the z12 tile doesn't carry the spot")
	}
	if n := len(tile(11)); n != 0 {
		t.Errorf("the z11 tile has %d bytes; spots start at z12", n)
	}

	if rec := d.do(account{}, "GET", "/tiles/v1/spots/14/0/0.mvt", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}
}
