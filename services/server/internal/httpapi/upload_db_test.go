package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/unpack"
)

const resumeGPX = `<?xml version="1.0"?>
<gpx><trk><trkseg>
<trkpt lat="50.0000" lon="10.0000"><time>2026-05-01T10:00:00Z</time></trkpt>
<trkpt lat="50.0010" lon="10.0010"><time>2026-05-01T10:01:00Z</time></trkpt>
<trkpt lat="50.0020" lon="10.0020"><time>2026-05-01T10:02:00Z</time></trkpt>
</trkseg></trk></gpx>`

// uploadRaw posts one file to the upload endpoint as the given account.
func (d *dbTest) uploadRaw(as account, name string, data []byte) *httptest.ResponseRecorder {
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
	return rec
}

func (d *dbTest) uploadFile(as account, name string, data []byte) uploadResponse {
	d.t.Helper()
	rec := d.uploadRaw(as, name, data)
	var resp uploadResponse
	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		d.t.Fatalf("upload: status %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		d.t.Fatal(err)
	}
	return resp
}

// latestIngestJob is the account's newest ingest job as the worker takes it: with its inline
// raw payload moved to storage (ingest.PromoteRaw), ready for ingest.Process.
func (d *dbTest) latestIngestJob(as account) ingest.Job {
	d.t.Helper()
	var id int64
	var payload []byte
	if err := d.pool.QueryRow(context.Background(),
		`SELECT id, payload FROM jobs WHERE user_id = $1 AND kind = 'ingest' ORDER BY id DESC LIMIT 1`, as.id).Scan(&id, &payload); err != nil {
		d.t.Fatal(err)
	}
	var j ingest.Job
	if err := json.Unmarshal(payload, &j); err != nil {
		d.t.Fatal(err)
	}
	if err := ingest.PromoteRaw(context.Background(), d.pool, d.srv.store, id, j.RawPayloadKey); err != nil {
		d.t.Fatal(err)
	}
	return j
}

// runUnpack runs the account's newest unpack job as the worker would, and returns the state it
// finished with.
func (d *dbTest) runUnpack(as account) unpack.State {
	d.t.Helper()
	ctx := context.Background()
	var id int64
	var payload []byte
	if err := d.pool.QueryRow(ctx,
		`SELECT id, payload FROM jobs WHERE user_id = $1 AND kind = 'unpack' ORDER BY id DESC LIMIT 1`, as.id).Scan(&id, &payload); err != nil {
		d.t.Fatal(err)
	}
	if err := unpack.Run(ctx, d.pool, d.srv.store, id, payload); err != nil {
		d.t.Fatal(err)
	}
	var j unpack.Job
	if err := d.pool.QueryRow(ctx, `SELECT payload FROM jobs WHERE id = $1`, id).Scan(&payload); err != nil {
		d.t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &j); err != nil {
		d.t.Fatal(err)
	}
	return j.State
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

// A zip larger than the in-memory multipart threshold is stored from the temp file it spilled
// to, not copied into memory. Measured as what the request allocates, which stays a fixed
// multiple of the threshold (the multipart reader's own buffer, grown to it before spilling)
// however large the archive; keeping the part in memory and copying it whole grew with the
// archive, here to about 500 MiB. The store discards what it's sent: an in-memory one would
// count its own copy of the archive against the request.
func TestLargeZipUploadIsReadInPlace(t *testing.T) {
	s3 := &discardS3{}
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "ride.gpx", Method: zip.Store})
	w.Write([]byte(resumeGPX))
	pad, _ := zw.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	pad.Write(make([]byte, 3*multipartMemoryBytes))
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
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	d.srv.ServeHTTP(rec, req)
	runtime.ReadMemStats(&after)

	var resp archiveUploadResponse
	d.decode(rec, http.StatusAccepted, &resp)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 6*multipartMemoryBytes {
		t.Errorf("the request allocated %d MiB for a %d MiB archive", allocated>>20, archive.Len()>>20)
	}
	if got := s3.received.Load(); got < int64(archive.Len()) {
		t.Errorf("the store received %d bytes of a %d-byte archive", got, archive.Len())
	}
}

// An archive upload is answered once the archive is stored, with one `unpack` job; the job
// enqueues its activity files as ingest jobs under the archive's batch, counts what it
// skipped or already had, and removes the archive.
func TestZipUploadIsUnpackedByTheWorker(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	ctx := context.Background()

	upload := func(files map[string]string) archiveUploadResponse {
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		for name, content := range files {
			w, _ := zw.Create(name)
			w.Write([]byte(content))
		}
		zw.Close()
		var resp archiveUploadResponse
		d.decode(d.uploadRaw(me, "export.zip", archive.Bytes()), http.StatusAccepted, &resp)
		if resp.Status != "zip_accepted" || resp.Batch == "" {
			t.Fatalf("response = %+v", resp)
		}
		if !s3.Has(unpack.Key(me.id, resp.Batch)) {
			t.Fatal("the archive wasn't stored")
		}
		var ingests int
		if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE user_id = $1 AND kind = 'ingest' AND payload->>'batch' = $2`, me.id, resp.Batch).Scan(&ingests); err != nil || ingests != 0 {
			t.Fatalf("ingest jobs before unpacking: %d, %v", ingests, err)
		}
		return resp
	}

	resp := upload(map[string]string{"rides/ride.gpx": resumeGPX, "notes.txt": "hello"})
	if st := d.runUnpack(me); st.Skipped != 1 || st.Already != 0 || st.Truncated {
		t.Fatalf("state = %+v, want notes.txt skipped", st)
	}
	if s3.Has(unpack.Key(me.id, resp.Batch)) {
		t.Fatal("the archive is still stored after unpacking")
	}
	j := d.latestIngestJob(me)
	if j.SourceDetail != "ride.gpx" || j.Source != "upload" || j.Batch != resp.Batch || j.BatchTitle != "export.zip" {
		t.Fatalf("ingest job = %+v", j)
	}
	if res, err := ingest.Process(ctx, d.pool, d.srv.store, j); err != nil || !res.Persisted {
		t.Fatalf("ingest: %+v, %v", res, err)
	}

	upload(map[string]string{"ride.gpx": resumeGPX})
	if st := d.runUnpack(me); st.Already != 1 || st.Skipped != 0 {
		t.Fatalf("state = %+v, want ride.gpx already imported", st)
	}
}

// An unpack cut off partway — a deploy — resumes from its saved cursor: the files it had
// already enqueued aren't enqueued a second time.
func TestUnpackResumesFromItsCursor(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for _, name := range []string{"a.gpx", "b.txt", "c.gpx"} {
		w, _ := zw.Create(name)
		w.Write([]byte(strings.Replace(resumeGPX, "<trk>", "<trk><name>"+name+"</name>", 1)))
	}
	zw.Close()
	var resp archiveUploadResponse
	d.decode(d.uploadRaw(me, "export.zip", archive.Bytes()), http.StatusAccepted, &resp)

	// The state an earlier run left when it was cut off after its first chunk: a.gpx
	// enqueued, one unit walked.
	if _, err := d.pool.Exec(ctx, `UPDATE jobs SET payload = jsonb_set(payload, '{state}', '{"cursor": 1}') WHERE user_id = $1 AND kind = 'unpack'`, me.id); err != nil {
		t.Fatal(err)
	}
	if st := d.runUnpack(me); st.Cursor != 3 || st.Skipped != 1 {
		t.Fatalf("state = %+v, want cursor 3 and b.txt skipped", st)
	}
	var names []string
	rows, err := d.pool.Query(ctx, `SELECT payload->>'source_detail' FROM jobs WHERE user_id = $1 AND kind = 'ingest' ORDER BY id`, me.id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	if strings.Join(names, ",") != "c.gpx" {
		t.Fatalf("ingest jobs = %v, want only c.gpx", names)
	}
}

// discardS3 accepts single and multipart PUTs, reading each body through without keeping it,
// and counts the bytes it was sent.
type discardS3 struct{ received atomic.Int64 }

func (s *discardS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "application/xml")
	switch _, starting := q["uploads"]; {
	case q.Has("location"):
		io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
	case r.Method == http.MethodPost && starting:
		io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPost:
		io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>test</Bucket><Key>k</Key><ETag>"0"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodPut:
		n, _ := io.Copy(io.Discard, r.Body)
		s.received.Add(n)
		w.Header().Set("ETag", `"`+q.Get("partNumber")+`"`)
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

// Creates racing each other still stop at the limit: each counts under a per-account lock.
func TestPrivateLocationLimitHoldsUnderConcurrency(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	var wg sync.WaitGroup
	for i := 0; i < maxPrivateLocations+10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d.do(me, http.MethodPost, "/v1/private-locations", map[string]any{"lat": 10 + float64(i)*0.01, "lon": 20, "radius_m": 200})
		}(i)
	}
	wg.Wait()
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM privacy_zones WHERE user_id = $1`, me.id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != maxPrivateLocations {
		t.Fatalf("%d locations, want %d", n, maxPrivateLocations)
	}
}

// Ingest drops a point no place on Earth has before it reaches the stored track: one
// latitude past the pole otherwise ended up in the trajectory, the tile index and the stats.
func TestIngestDropsImpossiblePoints(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	gpx := strings.Replace(resumeGPX, `<trkpt lat="50.0010" lon="10.0010">`, `<trkpt lat="95" lon="10.0010">`, 1)
	d.uploadFile(me, "pole.gpx", []byte(gpx))
	res, err := ingest.Process(context.Background(), d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil {
		t.Fatal(err)
	}
	var points int
	var maxLat float64
	if err := d.pool.QueryRow(context.Background(), `
		SELECT s.point_count, ST_YMax(a.trajectory)
		FROM activities a JOIN activity_streams s ON s.activity_id = a.id WHERE a.id = $1`, res.ActivityID).Scan(&points, &maxLat); err != nil {
		t.Fatal(err)
	}
	if points != 2 || maxLat > 90 {
		t.Fatalf("%d points, max latitude %v; want the 2 real points", points, maxLat)
	}
}

// Google Maps Timeline import is gone (ADR-0039): the sync endpoint refuses its source.
func TestSyncRefusesTimelineSource(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	body := map[string]any{"source": "timeline", "activities": []map[string]any{{
		"external_id":   "seg-1777629600-1777630200",
		"activity_type": "driving",
		"points": []map[string]any{
			{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
			{"lat": 50.01, "lon": 10.01, "time": "2026-05-01T10:05:00Z"},
		},
	}}}
	if code := d.do(me, http.MethodPost, "/v1/sync/activities", body).Code; code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", code)
	}
}

// A sync request answers without touching object storage: each new activity's job carries its
// raw payload inline, already-ingested ones are found in one query, and the worker's
// PromoteRaw moves the bytes to the raw key and clears them from the row.
func TestSyncEnqueuesRawPayloadsInline(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	ctx := context.Background()
	points := []map[string]any{
		{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
		{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
		{"lat": 50.002, "lon": 10.002, "time": "2026-05-01T10:02:00Z"},
	}
	sync := func(ids ...string) []syncActivityResult {
		var acts []map[string]any
		for _, id := range ids {
			acts = append(acts, map[string]any{"external_id": id, "activity_type": "walk", "points": points})
		}
		var resp syncActivitiesResponse
		d.decode(d.do(me, http.MethodPost, "/v1/sync/activities", map[string]any{"source": "healthconnect", "activities": acts}), http.StatusOK, &resp)
		return resp.Results
	}

	got := sync("first-walk", "../bad", "second-walk")
	for i, want := range []string{"enqueued", "rejected", "enqueued"} {
		if got[i].Status != want {
			t.Errorf("result %d: %s, want %s", i, got[i].Status, want)
		}
	}
	key := "raw/" + me.id + "/healthconnect/second-walk.json"
	if s3.Has(key) {
		t.Fatal("the request wrote the raw payload to storage")
	}
	var inline int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE user_id = $1 AND raw IS NOT NULL`, me.id).Scan(&inline); err != nil || inline != 2 {
		t.Fatalf("jobs with an inline payload: %d, %v", inline, err)
	}

	res, err := ingest.Process(ctx, d.pool, d.srv.store, d.latestIngestJob(me))
	if err != nil || !res.Persisted {
		t.Fatalf("ingest: %+v, %v", res, err)
	}
	if !s3.Has(key) {
		t.Fatal("PromoteRaw didn't write the raw payload")
	}
	var cleared bool
	if err := d.pool.QueryRow(ctx, `SELECT raw IS NULL FROM jobs WHERE user_id = $1 AND payload->>'external_id' = 'second-walk'`, me.id).Scan(&cleared); err != nil || !cleared {
		t.Fatalf("raw cleared: %v, %v", cleared, err)
	}

	got = sync("second-walk", "third-walk")
	for i, want := range []string{"already_processed", "enqueued"} {
		if got[i].Status != want {
			t.Errorf("repeat result %d: %s, want %s", i, got[i].Status, want)
		}
	}
}

// `POST /v1/sync/known` answers which of a phone's sessions the account has: ingested or still
// queued, and no longer once the activity is deleted. Only the asker's own, only that source.
func TestSyncKnownAnswersWhatTheAccountHas(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	other := d.newAccount(false)
	ctx := context.Background()
	points := []map[string]any{
		{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
		{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
	}
	sync := func(as account, source string, ids ...string) {
		var acts []map[string]any
		for _, id := range ids {
			acts = append(acts, map[string]any{"external_id": id, "activity_type": "walk", "points": points})
		}
		d.decode(d.do(as, http.MethodPost, "/v1/sync/activities", map[string]any{"source": source, "activities": acts}), http.StatusOK, nil)
	}
	known := func(ids ...string) []string {
		var resp syncKnownResponse
		d.decode(d.do(me, http.MethodPost, "/v1/sync/known", map[string]any{"source": "healthconnect", "external_ids": ids}), http.StatusOK, &resp)
		slices.Sort(resp.Known)
		return resp.Known
	}

	sync(me, "healthconnect", "ingested")
	job := d.latestIngestJob(me)
	res, err := ingest.Process(ctx, d.pool, d.srv.store, job)
	if err != nil || !res.Persisted {
		t.Fatalf("ingest: %+v, %v", res, err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE jobs SET state = 'done' WHERE user_id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	sync(me, "healthconnect", "queued")
	sync(me, "recorded", "other-source")
	sync(other, "healthconnect", "someone-elses")

	if got, want := known("ingested", "queued", "other-source", "someone-elses", "never-sent"), []string{"ingested", "queued"}; !slices.Equal(got, want) {
		t.Fatalf("known: %v, want %v", got, want)
	}

	if rec := d.do(me, http.MethodDelete, "/v1/activities/"+res.ActivityID, nil); rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if got, want := known("ingested", "queued"), []string{"queued"}; !slices.Equal(got, want) {
		t.Errorf("after delete: %v, want %v", got, want)
	}

	for _, body := range []map[string]any{
		{"source": "strava", "external_ids": []string{"x"}},
		{"source": "healthconnect", "external_ids": []string{"../x"}},
	} {
		if rec := d.do(me, http.MethodPost, "/v1/sync/known", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", body, rec.Code)
		}
	}
	demo := d.newAccount(true)
	if rec := d.do(demo, http.MethodPost, "/v1/sync/known", map[string]any{"source": "healthconnect", "external_ids": []string{"x"}}); rec.Code != http.StatusForbidden {
		t.Errorf("demo: %d, want 403", rec.Code)
	}
}

// A Google Takeout export goes the same way as a plain zip: recognised in the request, unpacked
// by the worker into one ingest job per activity with GPS, each typed and named as
// internal/takeout writes it.
func TestTakeoutUploadIsUnpackedByTheWorker(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	ctx := context.Background()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	root := "../takeout/testdata/sample"
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()

	var resp archiveUploadResponse
	d.decode(d.uploadRaw(me, "takeout-20260101.zip", archive.Bytes()), http.StatusAccepted, &resp)
	var format string
	if err := d.pool.QueryRow(ctx, `SELECT payload->>'format' FROM jobs WHERE user_id = $1 AND kind = 'unpack'`, me.id).Scan(&format); err != nil || format != unpack.FormatTakeout {
		t.Fatalf("unpack job format = %q, %v", format, err)
	}
	d.runUnpack(me)

	rows, err := d.pool.Query(ctx, `
		SELECT payload->>'source', payload->>'activity_type', payload->>'source_detail', payload->>'batch_title'
		FROM jobs WHERE user_id = $1 AND kind = 'ingest' ORDER BY id`, me.id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types := map[string]int{}
	for rows.Next() {
		var source, typ, name, title string
		if err := rows.Scan(&source, &typ, &name, &title); err != nil {
			t.Fatal(err)
		}
		if source != "takeout" || !strings.HasSuffix(name, ".gpx") || title != "takeout-20260101.zip" {
			t.Errorf("job: %s %s %s %s", source, typ, name, title)
		}
		types[typ]++
	}
	// The sample's two activities with GPS, as testdata/pathify-out has them.
	if len(types) != 2 || types["Walk"] != 1 || types["Outdoor Bike"] != 1 {
		t.Fatalf("ingest jobs by type: %v, want one Walk and one Outdoor Bike", types)
	}
}
