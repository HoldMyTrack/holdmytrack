package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// insertImportJob adds an ingest job the way persistAndEnqueue would, in the given state; a
// finished one gets a finished_at, as the worker sets it.
func (d *dbTest) insertImportJob(acct account, state, source, filename, batch, batchTitle string) {
	d.t.Helper()
	payload, err := json.Marshal(map[string]string{
		"user_id": acct.id, "source": source, "source_detail": filename, "external_id": filename,
		"batch": batch, "batch_title": batchTitle,
	})
	if err != nil {
		d.t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO jobs (kind, user_id, payload, state, finished_at)
		VALUES ('ingest', $1, $2, $3::text, CASE WHEN $3::text = 'pending' THEN NULL ELSE NOW() END)`,
		acct.id, payload, state); err != nil {
		d.t.Fatalf("insert job: %v", err)
	}
}

// The Upload menu's list: a single file is its own row, a batch is one row counting its
// finished jobs, and a batch or file with nothing pending left isn't listed. A phone sync is
// named after its source, not its platform id.
func TestActiveImports(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.insertImportJob(me, "pending", "upload", "walk.gpx", "", "")
	d.insertImportJob(me, "done", "upload", "old.gpx", "", "")
	d.insertImportJob(me, "done", "upload", "a.gpx", "b1", "Trips.zip")
	d.insertImportJob(me, "failed", "upload", "b.gpx", "b1", "Trips.zip")
	d.insertImportJob(me, "pending", "upload", "c.gpx", "b1", "Trips.zip")
	d.insertImportJob(me, "done", "upload", "d.gpx", "b2", "Finished.zip")
	d.insertImportJob(me, "pending", "healthconnect", "3f0c9a.json", "b3", "")

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	got := map[string][2]int64{}
	for _, a := range resp.Imports {
		got[a.Title] = [2]int64{a.Done, a.Total}
	}
	want := map[string][2]int64{"walk.gpx": {0, 1}, "Trips.zip": {2, 3}, "Health Connect": {0, 1}}
	if len(got) != len(want) {
		t.Fatalf("imports: %v, want %v", got, want)
	}
	for title, counts := range want {
		if got[title] != counts {
			t.Errorf("%s: done/total %v, want %v", title, got[title], counts)
		}
	}

	// Someone else's jobs never show.
	other := d.newAccount(false)
	d.decode(d.do(other, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if len(resp.Imports) != 0 || resp.UnseenFailures != 0 {
		t.Errorf("another account sees %+v", resp)
	}
}

// A failure that finished after the account last looked is unseen until opening /sync moves
// imports_seen_at past it.
func TestUnseenImportFailures(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET imports_seen_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	d.insertImportJob(me, "failed", "upload", "bad.gpx", "", "")
	d.insertImportJob(me, "done", "upload", "good.gpx", "", "")

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if resp.UnseenFailures != 1 {
		t.Fatalf("unseen failures %d, want 1", resp.UnseenFailures)
	}
	if res := d.do(me, "GET", "/sync", nil); res.Code != http.StatusOK {
		t.Fatalf("GET /sync: %d", res.Code)
	}
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if resp.UnseenFailures != 0 {
		t.Errorf("unseen failures after opening /sync %d, want 0", resp.UnseenFailures)
	}
}

// An import sent over several sync requests under one client batch is one row, titled with the
// batch's title; a malformed batch id is refused.
func TestSyncRequestsShareAClientBatch(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	send := func(id, batch string) int {
		body := map[string]any{"source": "timeline", "batch": batch, "batch_title": "Timeline.json", "activities": []map[string]any{{
			"external_id": id, "activity_type": "walking",
			"points": []map[string]any{
				{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
				{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
			},
		}}}
		return d.do(me, http.MethodPost, "/v1/sync/activities", body).Code
	}
	for _, id := range []string{"seg-1", "seg-2", "seg-3"} {
		if code := send(id, "k3Jd9xQa2LmP"); code != http.StatusOK {
			t.Fatalf("send %s: status %d", id, code)
		}
	}
	if code := send("seg-4", "../x"); code != http.StatusBadRequest {
		t.Errorf("malformed batch: status %d, want 400", code)
	}

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if len(resp.Imports) != 1 || resp.Imports[0].Title != "Timeline.json" || resp.Imports[0].Total != 3 {
		t.Errorf("imports: %+v, want one Timeline.json row of 3", resp.Imports)
	}
}
