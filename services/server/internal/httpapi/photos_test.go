package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// memS3 is an object store that keeps what's put in it, enough for minio-go's single-part
// PUT, GET and DELETE of one object and its bucket-location lookup.
type memS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemS3() *memS3 { return &memS3{objects: map[string][]byte{}} }

func (m *memS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["location"]; ok {
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/test/")
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			body = decodeAWSChunked(body)
		}
		m.objects[key] = body
		w.Header().Set("ETag", `"0"`)
	case http.MethodGet, http.MethodHead:
		body, ok := m.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		w.Header().Set("ETag", `"0"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			w.Write(body)
		}
	case http.MethodDelete:
		delete(m.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodeAWSChunked strips the chunk framing minio-go signs a plain-HTTP upload with:
// "<hex size>;chunk-signature=…\r\n<data>\r\n", ending with a zero-size chunk.
func decodeAWSChunked(body []byte) []byte {
	var out []byte
	for len(body) > 0 {
		line, rest, ok := bytes.Cut(body, []byte("\r\n"))
		if !ok {
			break
		}
		sizeHex, _, _ := bytes.Cut(line, []byte(";"))
		size, err := strconv.ParseInt(string(sizeHex), 16, 64)
		if err != nil || size == 0 || int64(len(rest)) < size {
			break
		}
		out = append(out, rest[:size]...)
		body = bytes.TrimPrefix(rest[size:], []byte("\r\n"))
	}
	return out
}

func (m *memS3) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// uploadPhoto posts a photo as as, with fields beside the two images.
func (d *dbTest) uploadPhoto(as account, file, thumb []byte, fields map[string]string) *httptest.ResponseRecorder {
	d.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for name, data := range map[string][]byte{"file": file, "thumb": thumb} {
		if data == nil {
			continue
		}
		part, _ := mw.CreateFormFile(name, name+".jpg")
		part.Write(data)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/v1/photos", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", bearerPrefix+as.session)
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)
	return rec
}

func near(a *float64, b float64) bool { return a != nil && math.Abs(*a-b) < 1e-6 }

// The test track: (10, 50) at 10:00:00 UTC to (10.001, 50.001) at 10:01:00, so its midpoint
// (10.0005, 50.0005) is 10:00:30.
var photoTrackStart = time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

func TestPhotoPlacement(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET timezone = 'Europe/Berlin' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	file, thumb := testJPEG(t, 400, 300), testJPEG(t, 32, 24)

	cases := []struct {
		name     string
		fields   map[string]string
		lon, lat float64 // 0: unplaced
	}{
		{"capture time, mid-track", map[string]string{"taken_at": "2026-05-01T10:00:30Z"}, 10.0005, 50.0005},
		{"capture time just before the start clamps to it", map[string]string{"taken_at": "2026-05-01T09:57:00Z"}, 10, 50},
		{"capture time well off the track", map[string]string{"taken_at": "2026-05-01T12:00:00Z"}, 0, 0},
		{"wall clock in the account's zone", map[string]string{"taken_local": "2026-05-01T12:00:30"}, 10.0005, 50.0005},
		{"wall clock in another zone", map[string]string{"taken_local": "2026-05-01T19:00:30"}, 10.0005, 50.0005},
		{"position only, near the track", map[string]string{"lon": "10.0006", "lat": "50.0004"}, 10.0005, 50.0005},
		{"position only, far from it", map[string]string{"lon": "11", "lat": "50"}, 0, 0},
		{"nothing to go by", nil, 0, 0},
	}
	for _, c := range cases {
		fields := map[string]string{"activity_id": walk}
		for k, v := range c.fields {
			fields[k] = v
		}
		var p photoJSON
		d.decode(d.uploadPhoto(me, file, thumb, fields), http.StatusCreated, &p)
		if c.lon == 0 {
			if p.Lon != nil || p.RouteAt != nil {
				t.Errorf("%s: placed at %v,%v (%v), want unplaced", c.name, *p.Lon, *p.Lat, p.RouteAt)
			}
			continue
		}
		if !near(p.Lon, c.lon) || !near(p.Lat, c.lat) {
			t.Errorf("%s: at %v,%v, want %v,%v", c.name, p.Lon, p.Lat, c.lon, c.lat)
		}
	}

	// Stored, and served back to the owner only.
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+walk, nil), http.StatusOK, &list)
	if len(list.Photos) != len(cases) {
		t.Fatalf("listed %d photos, want %d", len(list.Photos), len(cases))
	}
	first := list.Photos[0]
	if first.Width != 400 || first.Height != 300 || first.URL != "/v1/photos/"+first.ID {
		t.Errorf("first photo %+v", first)
	}
	rec := d.do(me, "GET", first.ThumbURL, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(rec.Body.Bytes(), thumb) {
		t.Errorf("thumb: %d %q, %d bytes, want %d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len(), len(thumb))
	}
	other := d.newAccount(false)
	for _, req := range []struct{ method, path string }{
		{"GET", "/v1/photos?activity=" + walk},
		{"GET", first.URL},
		{"PATCH", first.URL},
		{"DELETE", first.URL},
	} {
		if rec := d.do(other, req.method, req.path, map[string]any{}); rec.Code != http.StatusNotFound {
			t.Errorf("another account's %s %s: %d, want 404", req.method, req.path, rec.Code)
		}
	}
}

func TestPhotoEditsAndPrivacy(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	var p photoJSON
	d.decode(d.uploadPhoto(me, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{"activity_id": walk}), http.StatusCreated, &p)
	if p.Lon != nil {
		t.Fatalf("a photo with nothing to go by was placed")
	}

	// Put on the map by hand: snaps to the track, however far off.
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"position": map[string]float64{"lon": 10.01, "lat": 50.0}}), http.StatusOK, &p)
	if !near(p.Lon, 10.001) || !near(p.Lat, 50.001) {
		t.Errorf("placed by hand at %v,%v, want the track's end", p.Lon, p.Lat)
	}
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"position": map[string]float64{"lon": 10.0005, "lat": 50.0005}}), http.StatusOK, &p)
	if !near(p.Lon, 10.0005) || p.RouteAt == nil || !p.RouteAt.Equal(photoTrackStart.Add(30*time.Second)) {
		t.Errorf("moved to %v at %v, want the midpoint at 10:00:30", p.Lon, p.RouteAt)
	}

	// A Private location added afterwards takes it off the map, and keeps it in the list.
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO privacy_zones (user_id, center, radius_m) VALUES ($1, ST_MakePoint(10.0005, 50.0005)::geography, 50)
	`, me.id); err != nil {
		t.Fatal(err)
	}
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+walk, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 || list.Photos[0].Lon != nil || list.Photos[0].RouteAt == nil {
		t.Errorf("inside a Private location: %+v, want listed, with route_at but no position", list.Photos)
	}

	// Caption: trimmed, cleared by an empty one, bounded.
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"caption": "  The bridge  "}), http.StatusOK, &p)
	if p.Caption == nil || *p.Caption != "The bridge" {
		t.Errorf("caption %v", p.Caption)
	}
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"caption": ""}), http.StatusOK, &p)
	if p.Caption != nil {
		t.Errorf("caption %q after clearing", *p.Caption)
	}
	if rec := d.do(me, "PATCH", p.URL, map[string]any{"caption": strings.Repeat("x", maxPhotoCaptionLen+1)}); rec.Code != http.StatusBadRequest {
		t.Errorf("long caption: %d, want 400", rec.Code)
	}

	// Off the map again.
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"position": nil}), http.StatusOK, &p)
	if p.RouteAt != nil {
		t.Errorf("route_at %v after unplacing", p.RouteAt)
	}

	// Deleting removes both images.
	if !s3.has(photoKey(me.id, p.ID)) || !s3.has(photoThumbKey(me.id, p.ID)) {
		t.Fatalf("images not stored")
	}
	d.decode(d.do(me, "DELETE", p.URL, nil), http.StatusNoContent, nil)
	if s3.has(photoKey(me.id, p.ID)) || s3.has(photoThumbKey(me.id, p.ID)) {
		t.Errorf("images left behind after delete")
	}
	d.decode(d.do(me, "GET", p.URL, nil), http.StatusNotFound, nil)
}

func TestPhotoUploadRefusals(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)
	fields := map[string]string{"activity_id": walk}

	cases := []struct {
		name        string
		file, thumb []byte
		fields      map[string]string
		want        int
	}{
		{"not an image", []byte("GIF89a not really"), thumb, fields, http.StatusUnsupportedMediaType},
		{"text", []byte("hello"), thumb, fields, http.StatusUnsupportedMediaType},
		{"an original, not resized", testJPEG(t, maxPhotoSide+1, 10), thumb, fields, http.StatusUnprocessableEntity},
		{"a thumbnail that isn't one", file, testJPEG(t, maxPhotoThumbSide+1, 10), fields, http.StatusUnprocessableEntity},
		{"no thumbnail", file, nil, fields, http.StatusBadRequest},
		{"no activity", file, thumb, map[string]string{}, http.StatusNotFound},
		{"someone else's activity", file, thumb, map[string]string{"activity_id": d.newActivity(d.newAccount(false), testActivity{activityType: "walking"})}, http.StatusNotFound},
		{"bad capture time", file, thumb, map[string]string{"activity_id": walk, "taken_at": "yesterday"}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := d.uploadPhoto(me, c.file, c.thumb, c.fields); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, strings.TrimSpace(rec.Body.String()))
		}
	}

	if rec := d.uploadPhoto(d.newAccount(true), file, thumb, fields); rec.Code != http.StatusForbidden {
		t.Errorf("demo: %d, want 403", rec.Code)
	}

	// The account limit.
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO activity_photos (user_id, activity_id, content_type, thumb_content_type, width, height, bytes)
		SELECT $1, $2, 'image/jpeg', 'image/jpeg', 1, 1, 1 FROM generate_series(1, $3)
	`, me.id, walk, maxPhotosPerAccount); err != nil {
		t.Fatal(err)
	}
	if rec := d.uploadPhoto(me, file, thumb, fields); rec.Code != http.StatusConflict {
		t.Errorf("over the limit: %d, want 409", rec.Code)
	}
}

func TestPhotosFollowTheirActivity(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	ctx := context.Background()
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)

	// Two recordings of the same walk: one with a track, one without. The tracked one wins.
	tracked := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	bare := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart})
	var p photoJSON
	d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": bare, "taken_at": "2026-05-01T10:00:30Z"}), http.StatusCreated, &p)
	if p.Lon != nil {
		t.Fatalf("placed on an activity with no track")
	}
	if _, err := ingest.ResolveDuplicates(ctx, d.pool, me.id, photoTrackStart, 60); err != nil {
		t.Fatal(err)
	}
	d.decode(d.do(me, "GET", p.URL, nil), http.StatusOK, nil)
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+tracked, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 || list.Photos[0].ID != p.ID {
		t.Fatalf("the winner's photos: %+v, want the hidden copy's", list.Photos)
	}
	// Its capture time, kept from the upload, now places it on the winner's track.
	if !near(list.Photos[0].Lon, 10.0005) {
		t.Errorf("moved photo at %v, want the track's midpoint", list.Photos[0].Lon)
	}

	// Deleting the activity deletes its photos, images and all.
	d.decode(d.do(me, "DELETE", "/v1/activities/"+tracked, nil), http.StatusNoContent, nil)
	if s3.has(photoKey(me.id, p.ID)) || s3.has(photoThumbKey(me.id, p.ID)) {
		t.Errorf("images left behind after the activity's delete")
	}
	d.decode(d.do(me, "GET", p.URL, nil), http.StatusNotFound, nil)
}

func TestStoryPhotos(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)
	day1 := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	day2 := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart.Add(24 * time.Hour), at: &[2]float64{11, 50}})
	elsewhere := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart.Add(48 * time.Hour), at: &[2]float64{12, 50}})
	for _, a := range []string{day2, day1, elsewhere} {
		d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": a}), http.StatusCreated, nil)
	}
	var st story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Trip", "activity_ids": []string{day1, day2}}), http.StatusCreated, &st)

	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?story="+st.ID, nil), http.StatusOK, &list)
	if len(list.Photos) != 2 || list.Photos[0].ActivityID != day2 || list.Photos[1].ActivityID != day1 {
		t.Errorf("story photos %+v, want day 2's then day 1's (by upload; neither placed)", list.Photos)
	}
	for _, path := range []string{"/v1/photos?story=" + st.ID, "/v1/photos?story=nope"} {
		if rec := d.do(d.newAccount(false), "GET", path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s from another account: %d, want 404", path, rec.Code)
		}
	}
	if rec := d.do(me, "GET", "/v1/photos", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no activity or story: %d, want 400", rec.Code)
	}
}
