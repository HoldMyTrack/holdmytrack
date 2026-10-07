package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"
)

// "Show in this area" (§4.25): the places in a box, in the chosen categories, capped with the
// total reported, behind the session.
func TestSpotsInArea(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)

	// Three places in a box of open ocean no other test uses, one of them unnamed, and one
	// outside it.
	base := time.Now().UnixNano()
	var ids []int64
	for i, sp := range []struct {
		category, name string
		lon, lat       float64
	}{
		{"playground", "Test Play", -30.5, -30.5},
		{"playground", "", -30.4, -30.4},
		{"monument", "Test Statue", -30.3, -30.3},
		{"playground", "Elsewhere", -20.5, -30.5},
	} {
		var id int64
		if err := d.pool.QueryRow(ctx, `
			INSERT INTO spots (category, name, geom, osm_type, osm_id)
			VALUES ($1, NULLIF($2, ''), ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, 30)::geometry), 'node', $5)
			RETURNING id
		`, sp.category, sp.name, sp.lon, sp.lat, base+int64(i)).Scan(&id); err != nil {
			t.Fatalf("create spot: %v", err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = ANY($1)`, ids) })

	get := func(query string) spotsAreaResponse {
		t.Helper()
		var resp spotsAreaResponse
		d.decode(d.do(me, "GET", "/v1/spots?"+query, nil), http.StatusOK, &resp)
		return resp
	}
	const box = "bbox=-31,-31,-30,-30"

	all := get(box + "&categories=playground,monument")
	if all.Total != 3 || len(all.Spots) != 3 {
		t.Fatalf("got %d of %d places, want 3 of 3: %+v", len(all.Spots), all.Total, all.Spots)
	}
	if all.Spots[2].Name != nil {
		t.Errorf("the unnamed place should come last, got %+v", all.Spots)
	}
	if one := get(box + "&categories=monument"); one.Total != 1 || one.Spots[0].Category != "monument" {
		t.Errorf("monuments only: %+v", one)
	}

	for _, bad := range []string{
		"categories=playground",
		box,
		box + "&categories=cafe",
		"bbox=-31,-30,-30,-31&categories=playground", // south past north
		"bbox=-30,-31,-30,-30&categories=playground", // no width
		"bbox=a,b,c,d&categories=playground",
	} {
		if rec := d.do(me, "GET", "/v1/spots?"+bad, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("?%s: status %d, want 400", bad, rec.Code)
		}
	}
	if rec := d.do(account{}, "GET", fmt.Sprintf("/v1/spots?%s&categories=playground", box), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}
}

// A view across the antimeridian finds the places on both sides of it, whichever way its bounds
// come: unwrapped, east past 180 (the web's), or wrapped, west > east.
func TestSpotsInAreaAcrossTheAntimeridian(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)
	base := time.Now().UnixNano()
	var ids []int64
	for i, lon := range []float64{179.5, -179.5, 170.5} {
		var id int64
		if err := d.pool.QueryRow(ctx, `
			INSERT INTO spots (category, name, geom, osm_type, osm_id)
			VALUES ('playground', 'Test', ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($1, -40.5), 4326)::geography, 30)::geometry), 'node', $2)
			RETURNING id
		`, lon, base+int64(i)).Scan(&id); err != nil {
			t.Fatalf("create spot: %v", err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = ANY($1)`, ids) })

	for _, bbox := range []string{"179,-41,181,-40", "179,-41,-179,-40", "-181,-41,-179,-40"} {
		var resp spotsAreaResponse
		d.decode(d.do(me, "GET", "/v1/spots?categories=playground&bbox="+bbox, nil), http.StatusOK, &resp)
		got := []int64{}
		for _, sp := range resp.Spots {
			got = append(got, sp.ID)
		}
		slices.Sort(got)
		want := []int64{ids[0], ids[1]}
		slices.Sort(want)
		if resp.Total != 2 || !slices.Equal(got, want) {
			t.Errorf("bbox=%s: %d of %d places %v, want both sides of 180°: %v", bbox, len(got), resp.Total, got, want)
		}
	}
}

func TestParseBBox(t *testing.T) {
	area := func(w, e []float64) spotArea { return spotArea{south: -10, north: 10, west: w, east: e} }
	for raw, want := range map[string]spotArea{
		"10,-10,20,10":     area([]float64{10}, []float64{20}),
		"170,-10,190,10":   area([]float64{170, -180}, []float64{180, -170}),
		"170,-10,-170,10":  area([]float64{170, -180}, []float64{180, -170}),
		"-190,-10,-170,10": area([]float64{170, -180}, []float64{180, -170}),
		"530,-10,540,10":   area([]float64{170}, []float64{180}),
		"-200,-10,200,10":  area([]float64{-180}, []float64{180}),
		"-180,-10,180,10":  area([]float64{-180}, []float64{180}),
		"10,-100,20,100":   {south: -90, north: 90, west: []float64{10}, east: []float64{20}},
	} {
		if got, ok := parseBBox(raw); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("parseBBox(%q) = %+v, %v, want %+v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"10,10,20,-10", "10,-10,10,10", "1,2,3", "a,b,c,d", "NaN,0,1,1"} {
		if _, ok := parseBBox(raw); ok {
			t.Errorf("parseBBox(%q) accepted", raw)
		}
	}
}
