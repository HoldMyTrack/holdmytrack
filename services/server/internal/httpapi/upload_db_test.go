package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

const resumeGPX = `<?xml version="1.0"?>
<gpx><trk><trkseg>
<trkpt lat="50.0000" lon="10.0000"><time>2026-05-01T10:00:00Z</time></trkpt>
<trkpt lat="50.0010" lon="10.0010"><time>2026-05-01T10:01:00Z</time></trkpt>
<trkpt lat="50.0020" lon="10.0020"><time>2026-05-01T10:02:00Z</time></trkpt>
</trkseg></trk></gpx>`

func (d *dbTest) uploadFile(as account, name string, data []byte) uploadResponse {
	d.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", name)
	part.Write(data)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/activities/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", bearerPrefix+as.session)
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)
	var resp uploadResponse
	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		d.t.Fatalf("upload: status %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		d.t.Fatal(err)
	}
	return resp
}

func (d *dbTest) latestIngestJob(as account) ingest.Job {
	d.t.Helper()
	var payload []byte
	if err := d.pool.QueryRow(context.Background(),
		`SELECT payload FROM jobs WHERE user_id = $1 AND kind = 'ingest' ORDER BY id DESC LIMIT 1`, as.id).Scan(&payload); err != nil {
		d.t.Fatal(err)
	}
	var j ingest.Job
	if err := json.Unmarshal(payload, &j); err != nil {
		d.t.Fatal(err)
	}
	return j
}

// An ingest cut off after its activity row commits isn't "already processed": uploading the
// file again enqueues it, and the job finishes what the first one didn't.
func TestInterruptedIngestResumes(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()

	if resp := d.uploadFile(me, "ride.gpx", []byte(resumeGPX)); resp.Status != "enqueued" {
		t.Fatalf("first upload: %q", resp.Status)
	}
	res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil || !res.Persisted {
		t.Fatalf("first ingest: %+v, %v", res, err)
	}
	if resp := d.uploadFile(me, "ride.gpx", []byte(resumeGPX)); resp.Status != "already_processed" {
		t.Fatalf("repeat of a finished ingest: %q", resp.Status)
	}

	// The state a worker killed right after the insert leaves behind.
	for _, q := range []string{
		`UPDATE activities SET ingest_complete = false WHERE id = $1`,
		`DELETE FROM activity_streams WHERE activity_id = $1`,
		`DELETE FROM activity_tile_masks WHERE activity_id = $1`,
	} {
		if _, err := d.pool.Exec(ctx, q, res.ActivityID); err != nil {
			t.Fatal(err)
		}
	}

	if resp := d.uploadFile(me, "ride.gpx", []byte(resumeGPX)); resp.Status != "enqueued" {
		t.Fatalf("re-upload of an unfinished ingest: %q", resp.Status)
	}
	res2, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil || !res2.Persisted || res2.ActivityID != res.ActivityID {
		t.Fatalf("resumed ingest: %+v, %v", res2, err)
	}
	var complete bool
	var streams, masks int
	if err := d.pool.QueryRow(ctx, `
		SELECT a.ingest_complete,
		       (SELECT count(*) FROM activity_streams WHERE activity_id = a.id),
		       (SELECT count(*) FROM activity_tile_masks WHERE activity_id = a.id)
		FROM activities a WHERE a.id = $1`, res.ActivityID).Scan(&complete, &streams, &masks); err != nil {
		t.Fatal(err)
	}
	if !complete || streams != 1 || masks == 0 {
		t.Fatalf("after resume: complete %v, streams %d, masks %d", complete, streams, masks)
	}
}

// A zip larger than the in-memory multipart threshold is read from the temp file it spilled
// to, not copied into memory, and still unpacks.
func TestLargeZipUploadIsReadInPlace(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "ride.gpx", Method: zip.Store})
	w.Write([]byte(resumeGPX))
	pad, _ := zw.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	pad.Write(make([]byte, multipartMemoryBytes+1))
	zw.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "export.zip")
	part.Write(archive.Bytes())
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/activities/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", bearerPrefix+me.session)
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)

	var resp zipUploadResponse
	d.decode(rec, http.StatusAccepted, &resp)
	statuses := map[string]string{}
	for _, f := range resp.Files {
		statuses[f.Filename] = f.Status
	}
	if statuses["ride.gpx"] != "enqueued" || statuses["padding.bin"] != "skipped" {
		t.Fatalf("files = %+v", resp.Files)
	}
}

// A single file over maxUploadBytes is refused, not silently cut off at the limit.
func TestOversizedFileUploadIsRefused(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "huge.gpx")
	part.Write(make([]byte, maxUploadBytes+1))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/activities/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", bearerPrefix+me.session)
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
}

// A job's status is its owner's to read: another account asking after the same file (the
// same external_id, its SHA-256) learns nothing.
func TestActivityStatusIsPerAccount(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me, other := d.newAccount(false), d.newAccount(false)
	resp := d.uploadFile(me, "ride.gpx", []byte(resumeGPX))

	var mine, theirs statusResponse
	d.decode(d.do(me, http.MethodGet, "/v1/activities/status/"+resp.ExternalID, nil), http.StatusOK, &mine)
	d.decode(d.do(other, http.MethodGet, "/v1/activities/status/"+resp.ExternalID, nil), http.StatusOK, &theirs)
	if mine.Status != "processing" || theirs.Status != "unknown" {
		t.Fatalf("owner sees %q, another account %q; want processing, unknown", mine.Status, theirs.Status)
	}
}

// An activity older than Heatmap's window is out of it from the moment it's ingested, not
// only after the next daily sweep.
func TestOldActivityIngestsOutsideTheHeatmapWindow(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	old := strings.ReplaceAll(resumeGPX, "2026-05-01", "2019-05-01")
	d.uploadFile(me, "old.gpx", []byte(old))
	res, err := ingest.Process(context.Background(), d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil {
		t.Fatal(err)
	}
	var inWindow bool
	if err := d.pool.QueryRow(context.Background(),
		`SELECT in_heatmap_window FROM activities WHERE id = $1`, res.ActivityID).Scan(&inWindow); err != nil {
		t.Fatal(err)
	}
	if inWindow {
		t.Fatal("a 2019 activity was ingested inside the heatmap window")
	}
}

// A synced activity's external_id becomes part of an object key and of two VARCHAR(255)
// columns, so one that could nest the key or overflow them is rejected up front.
func TestSyncRejectsAnUnsafeExternalID(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	points := []map[string]any{
		{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
		{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
	}
	ids := []string{"../other", "a/b", strings.Repeat("x", 201), "3f2c9a1e-7b4d-4e8a-9c1f-2b6d8e0a4c57"}
	var acts []map[string]any
	for _, id := range ids {
		acts = append(acts, map[string]any{"external_id": id, "activity_type": "walk", "points": points})
	}
	var resp syncActivitiesResponse
	d.decode(d.do(me, http.MethodPost, "/v1/sync/activities", map[string]any{"source": "healthconnect", "activities": acts}), http.StatusOK, &resp)
	want := []string{"rejected", "rejected", "rejected", "enqueued"}
	for i, r := range resp.Results {
		if r.Status != want[i] {
			t.Errorf("%.20q: %s (%s), want %s", ids[i], r.Status, r.Error, want[i])
		}
	}
}

// An activity route given an id that isn't a UUID answers 404, not the 500 Postgres's refusal
// to parse it used to become.
func TestActivityRoutesRefuseMalformedIDs(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPatch, "/v1/activities/not-a-uuid"},
		{http.MethodDelete, "/v1/activities/not-a-uuid"},
		{http.MethodGet, "/v1/activities/track-metrics/not-a-uuid"},
		{http.MethodGet, "/v1/activities/track-points/not-a-uuid"},
		{http.MethodPost, "/v1/activities/track-edit/not-a-uuid"},
	} {
		if rec := d.do(me, tc.method, tc.path, map[string]any{}); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

// Coordinates that aren't a tile are a 400, not ST_TileEnvelope's error surfacing as a 500.
func TestTileRoutesRefuseOutOfRangeCoordinates(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	for _, path := range []string{
		"/tiles/v1/tracks/-1/0/0.mvt", "/tiles/v1/tracks/2/4/0.mvt", "/tiles/v1/tracks/2/0/-1.mvt",
		"/tiles/v1/tracks/40/0/0.mvt", "/tiles/v1/fog/3/8/0.png", "/tiles/v1/heatmap/1/0/2.png",
	} {
		if rec := d.do(me, http.MethodGet, path, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", path, rec.Code)
		}
	}
	if rec := d.do(me, http.MethodGet, "/tiles/v1/tracks/2/3/3.mvt", nil); rec.Code != http.StatusOK {
		t.Errorf("a real tile: %d, want 200", rec.Code)
	}
}
