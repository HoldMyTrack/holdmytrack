package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// A span is answered with the activity it overlaps by 80% of the longer of the two; a walk
// inside it and an empty span are not.
func TestActivityOverlaps(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	ctx := context.Background()
	at := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 3600, startedAt: at})
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET name = 'Morning walk' WHERE id = $1`, walk); err != nil {
		t.Fatal(err)
	}
	// Someone else's activity at the same time is never an answer.
	d.newActivity(d.newAccount(false), testActivity{activityType: "walking", durationSecs: 3600, startedAt: at})

	span := func(key string, from, to time.Duration) map[string]any {
		return map[string]any{"key": key, "start": at.Add(from), "end": at.Add(to)}
	}
	var resp overlapsResponse
	d.decode(d.do(me, http.MethodPost, "/v1/activities/overlaps", map[string]any{"spans": []map[string]any{
		span("copy", 2*time.Minute, 61*time.Minute),
		span("inside", 0, 10*time.Minute),
		span("empty", 0, 0),
	}}), http.StatusOK, &resp)
	if len(resp.Overlaps) != 1 {
		t.Fatalf("overlaps %+v, want only the copy", resp.Overlaps)
	}
	if o := resp.Overlaps[0]; o.Key != "copy" || o.ActivityID != walk || o.Name != "Morning walk" || o.ActivityType != "walking" {
		t.Errorf("overlap %+v, want copy → %s, Morning walk", o, walk)
	}

	if code := d.do(me, http.MethodPost, "/v1/activities/overlaps", map[string]any{"spans": []map[string]any{span("", 0, time.Hour)}}).Code; code != http.StatusBadRequest {
		t.Errorf("empty key: status %d, want 400", code)
	}
}

// The same walk synced twice from two sources is two live activities: ingest hides nothing.
func TestOverlappingSyncsStayLive(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()
	send := func(source, id string, offset time.Duration) string {
		t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC).Add(offset)
		body := map[string]any{"source": source, "activities": []map[string]any{{
			"external_id": id, "activity_type": "walking",
			"points": []map[string]any{
				{"lat": 50.0, "lon": 10.0, "time": t0},
				{"lat": 50.01, "lon": 10.01, "time": t0.Add(30 * time.Minute)},
				{"lat": 50.02, "lon": 10.02, "time": t0.Add(time.Hour)},
			},
		}}}
		var resp syncActivitiesResponse
		d.decode(d.do(me, http.MethodPost, "/v1/sync/activities", body), http.StatusOK, &resp)
		res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
		if err != nil {
			t.Fatal(err)
		}
		return res.ActivityID
	}
	a := send("healthconnect", "hc-1", 0)
	b := send("recorded", "rec-1", time.Minute)
	var live int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM activities WHERE id = ANY($1::uuid[])`, []string{a, b}).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 2 {
		t.Errorf("%d live, want both", live)
	}
}
