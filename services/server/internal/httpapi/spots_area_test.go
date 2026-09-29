package httpapi

import (
	"context"
	"fmt"
	"net/http"
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
		"bbox=-30,-31,-31,-30&categories=playground",
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
