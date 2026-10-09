package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// insertImportJob adds an ingest job the way ingest.EnqueueRaw would, in the given state; a
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

// An archive is one row from its upload on: "unpacking" while its unpack job is pending, with
// the ingest jobs it has made so far counted. Once that job has finished, its outcome is in
// `unpacked`, a failure's in the reader's language.
func TestActiveImportsUnpacking(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	ctx := context.Background()
	unpackJob := func(state, batch, title, code string, st map[string]any) {
		payload, _ := json.Marshal(map[string]any{
			"user_id": me.id, "source": "upload", "format": "zip", "batch": batch, "batch_title": title, "state": st,
		})
		if _, err := d.pool.Exec(ctx, `
			INSERT INTO jobs (kind, user_id, payload, state, error_code, finished_at)
			VALUES ('unpack', $1, $2, $3::text, NULLIF($4, ''), CASE WHEN $3::text = 'pending' THEN NULL ELSE NOW() END)`,
			me.id, payload, state, code); err != nil {
			t.Fatal(err)
		}
	}
	unpackJob("pending", "b1", "Trips.zip", "", map[string]any{"cursor": 0})
	d.insertImportJob(me, "pending", "upload", "a.gpx", "b1", "Trips.zip")
	unpackJob("done", "b2", "Old.zip", "", map[string]any{"cursor": 5, "already": 2, "skipped": 1, "truncated": true})
	unpackJob("failed", "b3", "Broken.zip", "unreadable_archive", map[string]any{"cursor": 0})

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if len(resp.Imports) != 1 || resp.Imports[0].Title != "Trips.zip" || !resp.Imports[0].Unpacking || resp.Imports[0].Total != 1 {
		t.Fatalf("imports = %+v, want Trips.zip unpacking with 1 found", resp.Imports)
	}
	byBatch := map[string]unpackedArchive{}
	for _, a := range resp.Unpacked {
		byBatch[a.Batch] = a
	}
	if a := byBatch["b2"]; len(resp.Unpacked) != 2 || a.Already != 2 || a.Skipped != 1 || !a.Truncated || a.Error != "" {
		t.Fatalf("unpacked = %+v", resp.Unpacked)
	}
	if a := byBatch["b3"]; a.Error != "The archive couldn't be read." {
		t.Fatalf("failed archive's error = %q", a.Error)
	}
}

// The history says when each import finished, and links a finished one to its activity.
func TestUploadHistoryFinishedAt(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.insertImportJob(me, "done", "upload", "walk.gpx", "", "")
	d.insertImportJob(me, "pending", "upload", "next.gpx", "", "")
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
