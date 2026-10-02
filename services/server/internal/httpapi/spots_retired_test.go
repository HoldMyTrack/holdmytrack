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

// A retired place (§4.25, ADR-0027): flagged in the tiles, returned by "Show in this area" and
// the detail only to an account that captured it, and capturable by no one.
func TestRetiredSpot(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	captor := d.newAccount(false)
	other := d.newAccount(false)

	at := [2]float64{-50.5, -50.5} // lon, lat: open ocean, clear of every other test's fixtures
	var id int64
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO spots (category, name, geom, osm_type, osm_id)
		VALUES ('playground', 'Test Gone', ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, 30)::geometry), 'node', $3)
		RETURNING id
	`, at[0], at[1], time.Now().UnixNano()).Scan(&id); err != nil {
		t.Fatalf("create spot: %v", err)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, id) })
	path := fmt.Sprintf("/v1/spots/%d", id)
	position := map[string]float64{"lat": at[1], "lon": at[0]}

	d.decode(d.do(captor, "POST", path+"/captures", position), http.StatusCreated, nil)
	if _, err := d.pool.Exec(ctx, `UPDATE spots SET retired_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("retire: %v", err)
	}

	x, y := tilemath.LonLatToTile(at[0], at[1], 14)
	rec := d.do(other, "GET", fmt.Sprintf("/tiles/v1/spots/14/%d/%d.mvt", x, y), nil)
	d.decode(rec, http.StatusOK, nil)
	if body := rec.Body.Bytes(); !bytes.Contains(body, []byte("Test Gone")) || !bytes.Contains(body, []byte("retired")) {
		t.Errorf("the tile should still carry the place, with its retired flag")
	}

	inArea := func(as account) spotsAreaResponse {
		t.Helper()
		var resp spotsAreaResponse
		d.decode(d.do(as, "GET", "/v1/spots?bbox=-51,-51,-50,-50&categories=playground", nil), http.StatusOK, &resp)
		return resp
	}
	if mine := inArea(captor); mine.Total != 1 || len(mine.Spots) != 1 || mine.Spots[0].ID != id {
		t.Errorf("Show in this area, captured: %+v", mine)
	}
	if theirs := inArea(other); theirs.Total != 0 || len(theirs.Spots) != 0 {
		t.Errorf("Show in this area, not captured: %+v", theirs)
	}

	var detail spotDetailJSON
	d.decode(d.do(captor, "GET", path, nil), http.StatusOK, &detail)
	if detail.ID != id || detail.CapturedAt == nil {
		t.Errorf("detail, captured: %+v", detail)
	}
	if rec := d.do(other, "GET", path, nil); rec.Code != http.StatusNotFound {
		t.Errorf("detail, not captured: status %d, want 404", rec.Code)
	}

	for _, as := range []account{captor, other} {
		if rec := d.do(as, "POST", path+"/captures", position); rec.Code != http.StatusGone {
			t.Errorf("capture: status %d, want 410", rec.Code)
		}
	}
}
