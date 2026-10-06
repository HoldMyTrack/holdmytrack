package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// Six points a minute apart, 10:00 to 10:05.
const splitGPX = `<?xml version="1.0"?>
<gpx><trk><trkseg>
<trkpt lat="50.0000" lon="10.0000"><time>2026-05-01T10:00:00Z</time></trkpt>
<trkpt lat="50.0010" lon="10.0010"><time>2026-05-01T10:01:00Z</time></trkpt>
<trkpt lat="50.0020" lon="10.0020"><time>2026-05-01T10:02:00Z</time></trkpt>
<trkpt lat="50.0030" lon="10.0030"><time>2026-05-01T10:03:00Z</time></trkpt>
<trkpt lat="50.0040" lon="10.0040"><time>2026-05-01T10:04:00Z</time></trkpt>
<trkpt lat="50.0050" lon="10.0050"><time>2026-05-01T10:05:00Z</time></trkpt>
</trkseg></trk></gpx>`

// runEditJob runs the account's newest edit_track job as the worker would.
func (d *dbTest) runEditJob(as account) {
	d.t.Helper()
	var payload []byte
	if err := d.pool.QueryRow(context.Background(),
		`SELECT payload FROM jobs WHERE user_id = $1 AND kind = 'edit_track' ORDER BY id DESC LIMIT 1`, as.id).Scan(&payload); err != nil {
		d.t.Fatal(err)
	}
	var j ingest.EditJob
	if err := json.Unmarshal(payload, &j); err != nil {
		d.t.Fatal(err)
	}
	if err := ingest.ProcessTrackEdit(context.Background(), d.pool, d.srv.store, j); err != nil {
		d.t.Fatal(err)
	}
}

func (d *dbTest) listActivities(as account) []activityRow {
	d.t.Helper()
	var resp activitiesResponse
	d.decode(d.do(as, "GET", "/v1/activities", nil), http.StatusOK, &resp)
	return resp.Activities
}

// Split one activity in two and merge it back (§4.7.8): each piece is a full activity with its
// own numbers and track, the original file still counts as imported, and merging restores the
// one activity it was.
func TestSplitAndMerge(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()

	if resp := d.uploadFile(me, "day.gpx", []byte(splitGPX)); resp.Status != "enqueued" {
		t.Fatalf("upload: %q", resp.Status)
	}
	res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil || !res.Persisted {
		t.Fatalf("ingest: %+v, %v", res, err)
	}
	orig := res.ActivityID
	var storyID string
	if err := d.pool.QueryRow(ctx, `INSERT INTO stories (user_id, name) VALUES ($1, 'Trip') RETURNING id`, me.id).Scan(&storyID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO story_activities (story_id, activity_id) VALUES ($1, $2)`, storyID, orig); err != nil {
		t.Fatal(err)
	}

	at := func(min int) int64 { return time.Date(2026, 5, 1, 10, min, 0, 0, time.UTC).UnixMilli() }
	editPath := "/v1/activities/track-edit/" + orig

	// One point on a side isn't a track.
	for _, m := range []int{0, 5} {
		if rec := d.do(me, "POST", editPath, map[string]any{"edit": nil, "split_at": at(m)}); rec.Code != http.StatusBadRequest {
			t.Errorf("split at 10:0%d: %d, want 400", m, rec.Code)
		}
	}
	// A Chop saved with the split applies to both pieces.
	chop := map[string]any{"keep": []int64{at(0), at(4)}}
	if rec := d.do(me, "POST", editPath, map[string]any{"edit": chop, "split_at": at(2)}); rec.Code != http.StatusAccepted {
		t.Fatalf("split: %d %s", rec.Code, rec.Body.String())
	}
	list := d.listActivities(me)
	if len(list) != 2 || !list[0].Pending || !list[1].Pending {
		t.Fatalf("after split: %+v, want two Pending pieces", list)
	}
	d.runEditJob(me)

	list = d.listActivities(me)
	if len(list) != 2 {
		t.Fatalf("after the job: %d activities", len(list))
	}
	second, first := list[0], list[1] // newest first
	if first.ID != orig || second.ID == orig {
		t.Fatalf("pieces %s, %s: the original should keep the earlier part", first.ID, second.ID)
	}
	for _, c := range []struct {
		row      activityRow
		start    int
		duration int32
	}{{first, 0, 120}, {second, 2, 120}} {
		if c.row.Pending || c.row.Split == nil || c.row.Split.Group != orig ||
			!c.row.StartedAt.Equal(time.UnixMilli(at(c.start))) ||
			c.row.DurationSeconds == nil || *c.row.DurationSeconds != c.duration ||
			len(c.row.Stories) != 1 {
			t.Errorf("piece %+v, want starting 10:0%d, %ds, in the Story", c.row, c.start, c.duration)
		}
	}
	if *first.Split.To != at(2) || first.Split.From != nil || *second.Split.From != at(2) || second.Split.To != nil {
		t.Errorf("ranges %+v / %+v, want meeting at 10:02", first.Split, second.Split)
	}

	// The editor opens on the piece's own points: 10:02 to 10:05, the Chop still in its spec.
	var pts trackPointsResponse
	d.decode(d.do(me, "GET", "/v1/activities/track-points/"+second.ID, nil), http.StatusOK, &pts)
	if len(pts.Points) != 4 || int64(pts.Points[0][2]) != at(2) || pts.Edit == nil || pts.Edit.Keep == nil {
		t.Errorf("second piece's points %v edit %+v", pts.Points, pts.Edit)
	}

	// The file is still the one already imported.
	if resp := d.uploadFile(me, "day.gpx", []byte(splitGPX)); resp.Status != "already_processed" {
		t.Errorf("re-upload after a split: %q", resp.Status)
	}

	// Merge: a lone piece, or a stranger's, is refused.
	if rec := d.do(me, "POST", "/v1/activities/track-merge", map[string]any{"ids": []string{orig}}); rec.Code != http.StatusConflict {
		t.Errorf("merge of one piece: %d, want 409", rec.Code)
	}
	other := d.newAccount(false)
	if rec := d.do(other, "POST", "/v1/activities/track-merge", map[string]any{"ids": []string{orig, second.ID}}); rec.Code != http.StatusConflict {
		t.Errorf("merge of someone else's pieces: %d, want 409", rec.Code)
	}
	var merged trackMergeResponse
	d.decode(d.do(me, "POST", "/v1/activities/track-merge", map[string]any{"ids": []string{second.ID, orig}}), http.StatusOK, &merged)
	if merged.ID != orig || len(merged.Removed) != 1 || merged.Removed[0] != second.ID {
		t.Fatalf("merge: %+v", merged)
	}
	d.runEditJob(me)

	list = d.listActivities(me)
	if len(list) != 1 {
		t.Fatalf("after merge: %d activities", len(list))
	}
	a := list[0]
	if a.ID != orig || a.Split != nil || a.Pending || a.DurationSeconds == nil || *a.DurationSeconds != 240 || len(a.Stories) != 1 {
		t.Errorf("merged %+v, want the original back, 10:00–10:04 (the Chop kept), in the Story", a)
	}
	var edit []byte
	if err := d.pool.QueryRow(ctx, `SELECT track_edit FROM activities WHERE id = $1`, orig).Scan(&edit); err != nil {
		t.Fatal(err)
	}
	var e ingest.TrackEdit
	if err := json.Unmarshal(edit, &e); err != nil || e.Keep == nil || e.Keep[1] != at(4) || len(e.Remove) != 0 {
		t.Errorf("merged spec %s, want just the Chop", edit)
	}
}

// A walk ending inside a Private location with a stray fix outside it as its last point, the
// fix deleted: a split among the points inside leaves the later part with no track. The server
// says so first (422), then splits when told to go ahead, the part kept with no geometry.
func TestSplitIntoPrivateLocation(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()
	const gpx = `<?xml version="1.0"?>
<gpx><trk><trkseg>
<trkpt lat="50.0000" lon="10.0000"><time>2026-05-01T10:00:00Z</time></trkpt>
<trkpt lat="50.0010" lon="10.0000"><time>2026-05-01T10:01:00Z</time></trkpt>
<trkpt lat="50.0020" lon="10.0000"><time>2026-05-01T10:02:00Z</time></trkpt>
<trkpt lat="50.0040" lon="10.0000"><time>2026-05-01T10:03:00Z</time></trkpt>
<trkpt lat="50.0045" lon="10.0000"><time>2026-05-01T10:04:00Z</time></trkpt>
<trkpt lat="50.0050" lon="10.0000"><time>2026-05-01T10:05:00Z</time></trkpt>
<trkpt lat="50.0100" lon="10.0000"><time>2026-05-01T10:06:00Z</time></trkpt>
</trkseg></trk></gpx>`
	// Home: 100 m around 50.0045, covering 10:03–10:05 but not the stray fix at 10:06.
	if _, err := d.pool.Exec(ctx, `INSERT INTO privacy_zones (user_id, center, radius_m)
		VALUES ($1, ST_SetSRID(ST_MakePoint(10.0, 50.0045), 4326)::geography, 100)`, me.id); err != nil {
		t.Fatal(err)
	}
	if resp := d.uploadFile(me, "home.gpx", []byte(gpx)); resp.Status != "enqueued" {
		t.Fatalf("upload: %q", resp.Status)
	}
	res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil || !res.Persisted {
		t.Fatalf("ingest: %+v, %v", res, err)
	}
	at := func(min int) int64 { return time.Date(2026, 5, 1, 10, min, 0, 0, time.UTC).UnixMilli() }
	path := "/v1/activities/track-edit/" + res.ActivityID
	dropStray := map[string]any{"drop": []int64{at(6)}}

	rec := d.do(me, "POST", path, map[string]any{"edit": dropStray, "split_at": at(4)})
	var body map[string]string
	d.decode(rec, http.StatusUnprocessableEntity, &body)
	if body["error"] != splitHidesPartCode || body["message"] == "" {
		t.Fatalf("hidden part: %v, want %s with a message", body, splitHidesPartCode)
	}
	if list := d.listActivities(me); len(list) != 1 || list[0].Pending {
		t.Fatalf("the warning changed something: %+v", list)
	}

	d.decode(d.do(me, "POST", path, map[string]any{"edit": dropStray, "split_at": at(4), "allow_hidden": true}), http.StatusAccepted, nil)
	d.runEditJob(me)
	list := d.listActivities(me)
	if len(list) != 2 {
		t.Fatalf("after split: %d activities", len(list))
	}
	later, earlier := list[0], list[1]
	if later.Pending || later.BBox != nil || !later.Private || (later.DistanceMeters != nil && *later.DistanceMeters != 0) {
		t.Errorf("later part %+v, want kept with no track", later)
	}
	if earlier.Pending || earlier.BBox == nil || earlier.Private {
		t.Errorf("earlier part %+v, want its track up to the location's edge", earlier)
	}
}
