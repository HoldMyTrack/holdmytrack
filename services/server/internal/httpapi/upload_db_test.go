package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
