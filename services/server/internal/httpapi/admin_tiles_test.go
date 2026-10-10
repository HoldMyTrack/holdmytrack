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

// The Country and Region tiles are drawn from outlines kept per tile (admin_tile_geoms): the
// first request builds a tile, which then serves exactly what drawing it live would, and a
// later one reads it back without building it again.
func TestAdminTilesAreBuiltOnceAndMatchTheLiveQuery(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	at := [2]float64{-24.75, -44.5} // lon, lat: open ocean in the South Atlantic

	// Two countries side by side, each a region of its own, the account's activity in the west.
	stamp := time.Now().UnixNano()
	var visited, unvisited, region, eastRegion int
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO admin_countries (code, name, geom)
		VALUES ($1, 'West', ST_Multi(ST_MakeEnvelope(-25, -45, -24.5, -44, 4326))) RETURNING id`,
		fmt.Sprintf("TW%d", stamp)).Scan(&visited); err != nil {
		t.Fatal(err)
	}
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO admin_countries (code, name, geom)
		VALUES ($1, 'East', ST_Multi(ST_MakeEnvelope(-24.5, -45, -24, -44, 4326))) RETURNING id`,
		fmt.Sprintf("TE%d", stamp)).Scan(&unvisited); err != nil {
		t.Fatal(err)
	}
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO admin_regions (country_id, overture_id, name, geom)
		VALUES ($1, $2, 'West', ST_Multi(ST_MakeEnvelope(-25, -45, -24.5, -44, 4326))) RETURNING id`,
		visited, fmt.Sprintf("test-region-w-%d", stamp)).Scan(&region); err != nil {
		t.Fatal(err)
	}
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO admin_regions (country_id, overture_id, name, geom)
		VALUES ($1, $2, 'East', ST_Multi(ST_MakeEnvelope(-24.5, -45, -24, -44, 4326))) RETURNING id`,
		unvisited, fmt.Sprintf("test-region-e-%d", stamp)).Scan(&eastRegion); err != nil {
		t.Fatal(err)
	}

	type tileCase struct {
		path    string
		layer   adminLayer
		queries adminTileQueries
	}
	cases := []tileCase{
		{"country-fog", countryLayer, countryFogQueries},
		{"country-heatmap", countryLayer, countryHeatmapQueries},
		{"region-fog", regionLayer, regionFogQueries},
		{"region-heatmap", regionLayer, regionHeatmapQueries},
	}
	coords := func(l adminLayer) (int, int, int) {
		x, y := tilemath.LonLatToTile(at[0], at[1], l.cacheMaxZoom)
		return l.cacheMaxZoom, x, y
	}
	forget := func() {
		for _, l := range []adminLayer{countryLayer, regionLayer} {
			z, x, y := coords(l)
			for _, table := range []string{"admin_tiles_built", "admin_tile_geoms"} {
				if _, err := d.pool.Exec(context.Background(),
					`DELETE FROM `+table+` WHERE layer = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`, l.name, z, x, y); err != nil {
					t.Error(err)
				}
			}
		}
	}
	forget()
	t.Cleanup(func() {
		forget()
		ctx := context.Background()
		d.pool.Exec(ctx, `DELETE FROM admin_regions WHERE id = ANY($1)`, []int{region, eastRegion})
		d.pool.Exec(ctx, `DELETE FROM admin_countries WHERE id = ANY($1)`, []int{visited, unvisited})
	})

	me := d.newAccount(false) // deleted, with its matches, before the outlines above
	activity := d.newActivity(me, testActivity{activityType: "walking", at: &at})
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE activities SET in_heatmap_window = true WHERE id = $1`, []any{activity}},
		{`INSERT INTO activity_country VALUES ($1, $2)`, []any{activity, visited}},
		{`INSERT INTO activity_region VALUES ($1, $2)`, []any{activity, region}},
	} {
		if _, err := d.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}

	get := func(c tileCase) []byte {
		t.Helper()
		z, x, y := coords(c.layer)
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/%s/%d/%d/%d.mvt", c.path, z, x, y), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", c.path, rec.Code)
		}
		return rec.Body.Bytes()
	}
	for _, c := range cases {
		served := get(c)
		z, x, y := coords(c.layer)
		var built bool
		var live []byte
		if err := d.pool.QueryRow(ctx, c.queries.live, z, x, y, me.id).Scan(&built, &live); err != nil {
			t.Fatalf("%s live: %v", c.path, err)
		}
		if len(served) == 0 || !bytes.Equal(served, live) {
			t.Errorf("%s: served %d bytes, drawing it live gives %d, want the same non-empty tile", c.path, len(served), len(live))
		}
	}

	// Built once: with its outlines taken away behind its back, the tile is read back empty
	// rather than drawn again.
	z, x, y := coords(countryLayer)
	if _, err := d.pool.Exec(ctx,
		`DELETE FROM admin_tile_geoms WHERE layer = 'countries' AND zoom = $1 AND tile_x = $2 AND tile_y = $3`, z, x, y); err != nil {
		t.Fatal(err)
	}
	if n := len(get(cases[0])); n != 0 {
		t.Errorf("a built tile was drawn again: %d bytes", n)
	}
}
