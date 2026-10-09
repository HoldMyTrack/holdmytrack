package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// insertImportJob adds an ingest job the way ingest.EnqueueRaw would, in the given state; a
// finished one gets a finished_at, as the worker sets it.
func (d *dbTest) insertImportJob(acct account, state, source, filename, batch string) {
	d.t.Helper()
	payload, err := json.Marshal(map[string]string{
		"user_id": acct.id, "source": source, "source_detail": filename, "external_id": filename,
		"batch": batch,
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
	d.insertImportJob(me, "pending", "upload", "walk.gpx", "")
	d.insertImportJob(me, "done", "upload", "old.gpx", "")
	d.insertImportJob(me, "done", "healthconnect", "a.json", "b1")
	d.insertImportJob(me, "failed", "healthconnect", "b.json", "b1")
	d.insertImportJob(me, "pending", "healthconnect", "c.json", "b1")
	d.insertImportJob(me, "done", "recorded", "d.json", "b2")

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	got := map[string][2]int64{}
	for _, a := range resp.Imports {
		got[a.Title] = [2]int64{a.Done, a.Total}
	}
	want := map[string][2]int64{"walk.gpx": {0, 1}, "Health Connect": {2, 3}}
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
	d.insertImportJob(me, "failed", "upload", "bad.gpx", "")
	d.insertImportJob(me, "done", "upload", "good.gpx", "")

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

// The history says when each import finished, and links a finished one to its activity.
func TestUploadHistoryFinishedAt(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.insertImportJob(me, "done", "upload", "walk.gpx", "")
	d.insertImportJob(me, "pending", "upload", "next.gpx", "")
	walk := d.newActivity(me, testActivity{activityType: "walk", distanceMeters: 1000})
	if _, err := d.pool.Exec(context.Background(), `UPDATE activities SET source = 'upload', external_id = 'walk.gpx' WHERE id = $1`, walk); err != nil {
		t.Fatal(err)
	}

	var resp uploadsResponse
	d.decode(d.do(me, "GET", "/v1/uploads?limit=10", nil), http.StatusOK, &resp)
	rows := map[string]uploadRow{}
	for _, u := range resp.Uploads {
		rows[u.Filename] = u
	}
	if w := rows["walk.gpx"]; w.Status != "done" || w.FinishedAt == nil || w.ActivityID == nil || *w.ActivityID != walk {
		t.Errorf("finished row: status %q, finished_at %v, activity %v; want done, set, %s", w.Status, w.FinishedAt, w.ActivityID, walk)
	}
	if n := rows["next.gpx"]; n.Status != "processing" || n.FinishedAt != nil {
		t.Errorf("pending row: status %q, finished_at %v; want processing with none", n.Status, n.FinishedAt)
	}
}
