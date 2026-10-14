package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// Capture (§4.25): a place's detail with its area, a capture checked against that area, kept
// once, listed per account, refused for a demo account, and gone with the account.
func TestSpotCaptures(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)
	other := d.newAccount(false)

	// A place mapped as a point — its 30 m circle — in open ocean no other test uses.
	var id int64
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO spots (category, name, geom, osm_type, osm_id)
		VALUES ('viewpoint', 'Test View', ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint(-40.5, -40.5), 4326)::geography, 30)::geometry), 'node', $1)
		RETURNING id
	`, time.Now().UnixNano()).Scan(&id); err != nil {
		t.Fatalf("create spot: %v", err)
	}
	t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, id) })
	path := fmt.Sprintf("/v1/spots/%d", id)

	var detail spotDetailJSON
	d.decode(d.do(me, "GET", path, nil), http.StatusOK, &detail)
	if detail.ID != id || detail.Category != "viewpoint" || detail.CapturedAt != nil || len(detail.Area) == 0 {
		t.Fatalf("detail before capture: %+v", detail)
	}

	// 1e-4° of latitude is about 11 m: inside the 30 m circle. 5e-4° is about 55 m: outside it,
	// and past the 10 m slack too.
	if rec := d.do(me, "POST", path+"/captures", map[string]float64{"lat": -40.5 - 5e-4, "lon": -40.5}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("outside: status %d, want 422", rec.Code)
	}
	var first, again spotCaptureJSON
	d.decode(d.do(me, "POST", path+"/captures", map[string]float64{"lat": -40.5 + 1e-4, "lon": -40.5}), http.StatusCreated, &first)
	d.decode(d.do(me, "POST", path+"/captures", map[string]float64{"lat": -40.5, "lon": -40.5}), http.StatusOK, &again)
	if !again.CapturedAt.Equal(first.CapturedAt) {
		t.Errorf("a second capture should keep the first one's time: %v, %v", first.CapturedAt, again.CapturedAt)
	}

	var mine, theirs spotCapturesResponse
	d.decode(d.do(me, "GET", "/v1/spots/captures", nil), http.StatusOK, &mine)
	d.decode(d.do(other, "GET", "/v1/spots/captures", nil), http.StatusOK, &theirs)
	if len(mine.Captures) != 1 || mine.Captures[0].SpotID != id {
		t.Errorf("my captures: %+v", mine)
	}
	if len(theirs.Captures) != 0 {
		t.Errorf("another account's captures: %+v", theirs)
	}
	d.decode(d.do(me, "GET", path, nil), http.StatusOK, &detail)
	if detail.CapturedAt == nil {
		t.Errorf("detail after capture should carry captured_at")
	}

	for _, bad := range []any{nil, map[string]float64{"lat": -40.5}, map[string]float64{"lat": 91, "lon": 0}} {
		if rec := d.do(me, "POST", path+"/captures", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("body %v: status %d, want 400", bad, rec.Code)
		}
	}
	for _, missing := range []string{"/v1/spots/0", "/v1/spots/abc", fmt.Sprintf("/v1/spots/%d", id+1_000_000_000)} {
		if rec := d.do(me, "GET", missing, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", missing, rec.Code)
		}
	}
	if rec := d.do(d.newAccount(true), "POST", path+"/captures", map[string]float64{"lat": -40.5, "lon": -40.5}); rec.Code != http.StatusForbidden {
		t.Errorf("demo: status %d, want 403", rec.Code)
	}
	if rec := d.do(account{}, "GET", "/v1/spots/captures", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}

	if _, err := d.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, me.id); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	var left int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM spot_captures WHERE spot_id = $1`, id).Scan(&left); err != nil || left != 0 {
		t.Errorf("captures after the account is deleted: %d (%v), want 0", left, err)
	}
}

func TestCapturesSince(t *testing.T) {
	if got, err := capturesSince(""); got != nil || err != nil {
		t.Errorf("absent: %v, %v; want the whole list", got, err)
	}
	got, err := capturesSince("2026-10-14T09:30:05.123456Z")
	if err != nil || !got.Equal(time.Date(2026, 10, 14, 9, 30, 5, 123456000, time.UTC)) {
		t.Errorf("a time: %v, %v", got, err)
	}
	if _, err := capturesSince("yesterday"); err == nil {
		t.Error("not a time: accepted")
	}
}

// With since, only the captures at or after it come back: the newest one the client already
// has, and anything newer.
func TestSpotCapturesSince(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)
	base := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	var ids []int64
	for i := range 3 {
		var id int64
		if err := d.pool.QueryRow(ctx, `
			INSERT INTO spots (category, name, geom, osm_type, osm_id)
			VALUES ('viewpoint', 'Test View', ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint(-41.5, -41.5), 4326)::geography, 30)::geometry), 'node', $1)
			RETURNING id`, time.Now().UnixNano()+int64(i)).Scan(&id); err != nil {
			t.Fatalf("create spot: %v", err)
		}
		t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, id) })
		if _, err := d.pool.Exec(ctx, `INSERT INTO spot_captures (user_id, spot_id, captured_at) VALUES ($1, $2, $3)`,
			me.id, id, base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	var all, newer spotCapturesResponse
	d.decode(d.do(me, "GET", "/v1/spots/captures", nil), http.StatusOK, &all)
	if len(all.Captures) != 3 || all.Captures[0].SpotID != ids[2] {
		t.Fatalf("whole list: %+v", all.Captures)
	}
	since := url.QueryEscape(base.Add(time.Hour).Format(time.RFC3339Nano))
	d.decode(d.do(me, "GET", "/v1/spots/captures?since="+since, nil), http.StatusOK, &newer)
	if len(newer.Captures) != 2 || newer.Captures[0].SpotID != ids[2] || newer.Captures[1].SpotID != ids[1] {
		t.Fatalf("since the second: %+v, want the second and third", newer.Captures)
	}
	if rec := d.do(me, "GET", "/v1/spots/captures?since=yesterday", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a since that isn't a time: status %d, want 400", rec.Code)
	}
}
