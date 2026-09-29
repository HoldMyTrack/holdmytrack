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

// The spots tiles (§4.25) carry the same places for everyone, with each caller's own visited
// flag, only from zoom 14 up. The flag isn't decoded here: a tile whose spot the caller has
// visited differs from the same tile for an account that hasn't, and stops differing once the
// visit's activity is superseded.
func TestSpotsTile(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me, them := d.newAccount(false), d.newAccount(false)

	at := [2]float64{-40.5, -20.5} // lon, lat: open ocean, clear of every other test's fixtures
	name := fmt.Sprintf("Test Monument %d", time.Now().UnixNano())
	var spotID int64
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO spots (category, name, geom, osm_type, osm_id)
		VALUES ('monument', $1, ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 50)::geometry), 'node', $4)
		RETURNING id
	`, name, at[0], at[1], time.Now().UnixNano()).Scan(&spotID); err != nil {
		t.Fatalf("create spot: %v", err)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, spotID) })

	activity := d.newActivity(me, testActivity{activityType: "walking", at: &at})
	if _, err := d.pool.Exec(ctx, `
		INSERT INTO spot_visits (user_id, spot_id, activity_id, visited_at, dwell_seconds) VALUES ($1, $2, $3, NOW(), 600)
	`, me.id, spotID, activity); err != nil {
		t.Fatalf("create visit: %v", err)
	}

	tile := func(as account, z int) []byte {
		t.Helper()
		x, y := tilemath.LonLatToTile(at[0], at[1], z)
		rec := d.do(as, "GET", fmt.Sprintf("/tiles/v1/spots/%d/%d/%d.mvt", z, x, y), nil)
		d.decode(rec, http.StatusOK, nil)
		return rec.Body.Bytes()
	}

	mine, theirs := tile(me, 14), tile(them, 14)
	if !bytes.Contains(mine, []byte(name)) || !bytes.Contains(theirs, []byte(name)) {
		t.Fatalf("the z14 tile doesn't carry the spot: mine %d bytes, theirs %d bytes", len(mine), len(theirs))
	}
	if bytes.Equal(mine, theirs) {
		t.Errorf("my tile, with the spot visited, is the same as theirs")
	}
	if n := len(tile(me, 13)); n != 0 {
		t.Errorf("the z13 tile has %d bytes; spots start at z14", n)
	}

	// A superseded duplicate's visit doesn't count.
	winner := d.newActivity(me, testActivity{activityType: "walking"})
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET superseded_by = $1 WHERE id = $2`, winner, activity); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if !bytes.Equal(tile(me, 14), theirs) {
		t.Errorf("a superseded activity's visit still marks the spot visited")
	}

	if rec := d.do(account{}, "GET", "/tiles/v1/spots/14/0/0.mvt", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}
}
