package httpapi

import (
	"context"
	"io"
	"log/slog"
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


// A walk from home is compared as recorded: the minutes its Private location clipped off the
// start still count, so the other source's full copy of it is marked — also once an activity
// stored before the recorded span existed has been backfilled.
func TestOverlapIgnoresPrivateLocationClip(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()
	// Home: 100 m around the start, where the first 6 of the walk's 20 minutes are spent.
	if _, err := d.pool.Exec(ctx, `INSERT INTO privacy_zones (user_id, center, radius_m)
		VALUES ($1, ST_SetSRID(ST_MakePoint(10.0, 50.0), 4326)::geography, 100)`, me.id); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 10, 14, 19, 0, 0, time.UTC)
	point := func(lat float64, m int) map[string]any {
		return map[string]any{"lat": lat, "lon": 10.0, "time": t0.Add(time.Duration(m) * time.Minute)}
	}
	body := map[string]any{"source": "recorded", "activities": []map[string]any{{
		"external_id": "rec-1", "activity_type": "walking",
		"points": []map[string]any{
			point(50.0, 0), point(50.0003, 3), point(50.0006, 6),
			point(50.003, 7), point(50.006, 12), point(50.009, 16), point(50.012, 20),
		},
	}}}
	var synced syncActivitiesResponse
	d.decode(d.do(me, http.MethodPost, "/v1/sync/activities", body), http.StatusOK, &synced)
	res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil {
		t.Fatal(err)
	}
	var started time.Time
	if err := d.pool.QueryRow(ctx, `SELECT started_at FROM activities WHERE id = $1`, res.ActivityID).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if !started.After(t0.Add(5 * time.Minute)) {
		t.Fatalf("started_at %v, want the clipped start after %v", started, t0.Add(5*time.Minute))
	}

	overlaps := func() []overlapJSON {
		var resp overlapsResponse
		d.decode(d.do(me, http.MethodPost, "/v1/activities/overlaps", map[string]any{"spans": []map[string]any{
			{"key": "hc", "start": t0, "end": t0.Add(20 * time.Minute)},
		}}), http.StatusOK, &resp)
		return resp.Overlaps
	}
	if o := overlaps(); len(o) != 1 || o[0].ActivityID != res.ActivityID {
		t.Fatalf("overlaps %+v, want the recorded walk", o)
	}

	// Stored before the columns existed, it's compared by its clipped track and missed...
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET recorded_started_at = NULL, recorded_ended_at = NULL WHERE id = $1`, res.ActivityID); err != nil {
		t.Fatal(err)
	}
	if o := overlaps(); len(o) != 0 {
		t.Fatalf("overlaps %+v before the backfill, want none", o)
	}
	// ...until the backfill reads its span back from the upload.
	if err := ingest.BackfillRecordedSpans(ctx, d.pool, d.srv.store, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if o := overlaps(); len(o) != 1 || o[0].ActivityID != res.ActivityID {
		t.Fatalf("overlaps %+v after the backfill, want the recorded walk", o)
	}
}
