package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

// memS3 is storagetest's in-memory object store.
type memS3 = storagetest.MemS3

func newMemS3() *memS3 { return storagetest.New() }

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

// A camera clock with no zone is read in the zone the activity was recorded in (§4.30), not the
// account's: placed by the user's choice, a time that's on no part of the track is kept as that
// zone's wall clock. (Two days before the track, no UTC offset puts it on the track.)
func TestPhotoWallClockInTheActivitysZone(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET timezone = 'Europe/Berlin' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart,
		at: &[2]float64{10, 50}, timezone: "Asia/Tokyo"})
	var p photoJSON
	d.decode(d.uploadPhoto(me, testJPEG(t, 400, 300), testJPEG(t, 32, 24), map[string]string{
		"activity_id": walk, "taken_local": "2026-04-29T08:00:00", "route_at": "2026-05-01T10:00:30Z",
	}), http.StatusCreated, &p)
	if want := time.Date(2026, 4, 28, 23, 0, 0, 0, time.UTC); p.TakenAt == nil || !p.TakenAt.Equal(want) {
		t.Errorf("taken at %v, want %v (08:00 in Tokyo)", p.TakenAt, want)
	}
}

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
		lon, lat float64 // 0: refused until the user places it
	}{
		{"capture time, mid-track", map[string]string{"taken_at": "2026-05-01T10:00:30Z"}, 10.0005, 50.0005},
		{"capture time just before the start clamps to it", map[string]string{"taken_at": "2026-05-01T09:57:00Z"}, 10, 50},
		{"capture time well off the track", map[string]string{"taken_at": "2026-05-01T12:00:00Z"}, 0, 0},
		{"wall clock in the activity's zone", map[string]string{"taken_local": "2026-05-01T12:00:30"}, 10.0005, 50.0005},
		{"wall clock in another zone", map[string]string{"taken_local": "2026-05-01T19:00:30"}, 10.0005, 50.0005},
		{"position only, near the track", map[string]string{"lon": "10.0006", "lat": "50.0004"}, 10.0005, 50.0005},
		{"position only, far from it", map[string]string{"lon": "11", "lat": "50"}, 0, 0},
		{"nothing to go by", nil, 0, 0},
		{"the user's choice", map[string]string{"route_at": "2026-05-01T10:00:30Z"}, 10.0005, 50.0005},
		{"the user's choice beats the capture time", map[string]string{"taken_at": "2026-05-01T10:00:00Z", "route_at": "2026-05-01T10:01:00Z"}, 10.001, 50.001},
		{"the user's choice past the end clamps to it", map[string]string{"route_at": "2026-05-01T11:00:00Z"}, 10.001, 50.001},
	}
	placed := 0
	for _, c := range cases {
		fields := map[string]string{"activity_id": walk}
		for k, v := range c.fields {
			fields[k] = v
		}
		rec := d.uploadPhoto(me, file, thumb, fields)
		if c.lon == 0 {
			var refusal map[string]string
			d.decode(rec, http.StatusUnprocessableEntity, &refusal)
			if refusal["error"] != photoNeedsPlaceCode || refusal["message"] == "" {
				t.Errorf("%s: refusal %v, want %s with a message", c.name, refusal, photoNeedsPlaceCode)
			}
			continue
		}
		var p photoJSON
		d.decode(rec, http.StatusCreated, &p)
		placed++
		if !near(p.Lon, c.lon) || !near(p.Lat, c.lat) {
			t.Errorf("%s: at %v,%v, want %v,%v", c.name, p.Lon, p.Lat, c.lon, c.lat)
		}
	}

	// Stored, in route order, and served back to the owner only.
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+walk, nil), http.StatusOK, &list)
	if len(list.Photos) != placed {
		t.Fatalf("listed %d photos, want %d (refused ones aren't kept)", len(list.Photos), placed)
	}
	for i := 1; i < len(list.Photos); i++ {
		if list.Photos[i].RouteAt.Before(list.Photos[i-1].RouteAt) {
			t.Errorf("photos out of route order: %+v", list.Photos)
		}
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

func TestPhotoEditsAndTrackChanges(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	ctx := context.Background()
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	var p photoJSON
	d.decode(d.uploadPhoto(me, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{"activity_id": walk, "taken_at": "2026-05-01T10:00:00Z"}), http.StatusCreated, &p)

	// The slider's track: the display points with their moments.
	var metrics trackMetricsResponse
	d.decode(d.do(me, "GET", "/v1/activities/track-metrics/"+walk, nil), http.StatusOK, &metrics)
	if len(metrics.Points) != 2 || metrics.Points[0].TimeS != photoTrackStart.Unix() || metrics.Points[1].TimeS != photoTrackStart.Unix()+60 {
		t.Errorf("track metrics %+v, want the two points' moments", metrics.Points)
	}

	// Moved along the track (the Edit window's slider), clamped to it.
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"route_at": "2026-05-01T10:00:30Z"}), http.StatusOK, &p)
	if !near(p.Lon, 10.0005) || !p.RouteAt.Equal(photoTrackStart.Add(30*time.Second)) {
		t.Errorf("moved to %v at %v, want the midpoint at 10:00:30", p.Lon, p.RouteAt)
	}
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"route_at": "2026-05-01T09:00:00Z"}), http.StatusOK, &p)
	if !near(p.Lon, 10) || !p.RouteAt.Equal(photoTrackStart) {
		t.Errorf("moved before the start: %v at %v, want the start", p.Lon, p.RouteAt)
	}
	for _, bad := range []any{nil, "noon"} {
		if rec := d.do(me, "PATCH", p.URL, map[string]any{"route_at": bad}); rec.Code != http.StatusBadRequest {
			t.Errorf("route_at %v: %d, want 400", bad, rec.Code)
		}
	}
	d.decode(d.do(me, "PATCH", p.URL, map[string]any{"route_at": "2026-05-01T10:00:30Z"}), http.StatusOK, &p)

	// A track that loses its second half (Edit track, or a Private location over its end):
	// the photo stays on what's left, at its end.
	if _, err := d.pool.Exec(ctx, `
		UPDATE activities SET trajectory = ST_GeomFromText($2, 4326) WHERE id = $1
	`, walk, fmt.Sprintf("LINESTRINGM(10 50 %d, 10.0002 50.0002 %d)", photoTrackStart.Unix(), photoTrackStart.Unix()+10)); err != nil {
		t.Fatal(err)
	}
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+walk, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 || !near(list.Photos[0].Lon, 10.0002) || !list.Photos[0].RouteAt.Equal(p.RouteAt) {
		t.Errorf("after the track was cut: %+v, want at the new end with route_at kept", list.Photos)
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

	// Deleting removes both images.
	if !s3.Has(photoKey(me.id, p.ID)) || !s3.Has(photoThumbKey(photoKey(me.id, p.ID))) {
		t.Fatalf("images not stored")
	}
	d.decode(d.do(me, "DELETE", p.URL, nil), http.StatusNoContent, nil)
	if s3.Has(photoKey(me.id, p.ID)) || s3.Has(photoThumbKey(photoKey(me.id, p.ID))) {
		t.Errorf("images left behind after delete")
	}
	d.decode(d.do(me, "GET", p.URL, nil), http.StatusNotFound, nil)
}

func TestPhotoUploadRefusals(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)
	fields := map[string]string{"activity_id": walk, "route_at": "2026-05-01T10:00:30Z"}

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
		{"bad place", file, thumb, map[string]string{"activity_id": walk, "route_at": "here"}, http.StatusBadRequest},
		{"an activity with no track", file, thumb, map[string]string{"activity_id": d.newActivity(me, testActivity{activityType: "walking"}), "route_at": "2026-05-01T10:00:30Z"}, http.StatusConflict},
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
		INSERT INTO activity_photos (user_id, activity_id, route_at, content_type, thumb_content_type, width, height, bytes, image_key)
		SELECT $1::uuid, $2, NOW(), 'image/jpeg', 'image/jpeg', 1, 1, 1, 'photos/' || $1::uuid::text || '/' || gen_random_uuid() FROM generate_series(1, $3)
	`, me.id, walk, maxPhotosPerAccount); err != nil {
		t.Fatal(err)
	}
	if rec := d.uploadPhoto(me, file, thumb, fields); rec.Code != http.StatusConflict {
		t.Errorf("over the limit: %d, want 409", rec.Code)
	}
}

func TestPhotosGoWithTheirActivity(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)

	tracked := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	var p photoJSON
	d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": tracked, "taken_at": "2026-05-01T10:00:30Z"}), http.StatusCreated, &p)
	d.decode(d.do(me, "GET", p.URL, nil), http.StatusOK, nil)

	// Deleting the activity deletes its photos, images and all.
	d.decode(d.do(me, "DELETE", "/v1/activities/"+tracked, nil), http.StatusNoContent, nil)
	if s3.Has(photoKey(me.id, p.ID)) || s3.Has(photoThumbKey(photoKey(me.id, p.ID))) {
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
	for _, a := range []struct {
		id string
		at time.Time
	}{{day2, photoTrackStart.Add(24 * time.Hour)}, {day1, photoTrackStart}, {elsewhere, photoTrackStart.Add(48 * time.Hour)}} {
		d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": a.id, "route_at": a.at.Format(time.RFC3339)}), http.StatusCreated, nil)
	}
	var st story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Trip", "activity_ids": []string{day1, day2}}), http.StatusCreated, &st)

	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?story="+st.ID, nil), http.StatusOK, &list)
	if len(list.Photos) != 2 || list.Photos[0].ActivityID != day1 || list.Photos[1].ActivityID != day2 {
		t.Errorf("story photos %+v, want day 1's then day 2's", list.Photos)
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

func TestPhotoPlaceCheck(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	bare := d.newActivity(me, testActivity{activityType: "walking"})
	lon, lat := 10.0006, 50.0004

	cases := []struct {
		name string
		body map[string]any
		want int
		at   time.Time
	}{
		{"capture time", map[string]any{"activity_id": walk, "taken_at": "2026-05-01T10:00:30Z"}, http.StatusOK, photoTrackStart.Add(30 * time.Second)},
		{"position", map[string]any{"activity_id": walk, "lon": lon, "lat": lat}, http.StatusOK, photoTrackStart.Add(30 * time.Second)},
		{"nothing to go by", map[string]any{"activity_id": walk}, http.StatusUnprocessableEntity, time.Time{}},
		{"no track", map[string]any{"activity_id": bare, "taken_at": "2026-05-01T10:00:30Z"}, http.StatusConflict, time.Time{}},
		{"someone else's activity", map[string]any{"activity_id": d.newActivity(d.newAccount(false), testActivity{activityType: "walking"})}, http.StatusNotFound, time.Time{}},
		{"bad capture time", map[string]any{"activity_id": walk, "taken_at": "noon"}, http.StatusBadRequest, time.Time{}},
	}
	for _, c := range cases {
		rec := d.do(me, "POST", "/v1/photos/place", c.body)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, strings.TrimSpace(rec.Body.String()))
			continue
		}
		if c.want == http.StatusOK {
			var got placeResponse
			d.decode(rec, http.StatusOK, &got)
			if !got.RouteAt.Equal(c.at) {
				t.Errorf("%s: route_at %v, want %v", c.name, got.RouteAt, c.at)
			}
		}
	}
	// It stores nothing.
	var list photosResponse
	d.decode(d.do(me, "GET", "/v1/photos?activity="+walk, nil), http.StatusOK, &list)
	if len(list.Photos) != 0 {
		t.Errorf("the check stored %d photos", len(list.Photos))
	}
	if rec := d.do(d.newAccount(true), "POST", "/v1/photos/place", map[string]any{"activity_id": walk}); rec.Code != http.StatusForbidden {
		t.Errorf("demo: %d, want 403", rec.Code)
	}

	// An upload carries its caption, trimmed.
	var p photoJSON
	d.decode(d.uploadPhoto(me, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{
		"activity_id": walk, "route_at": "2026-05-01T10:00:30Z", "caption": "  The bridge  ",
	}), http.StatusCreated, &p)
	if p.Caption == nil || *p.Caption != "The bridge" {
		t.Errorf("uploaded caption %v", p.Caption)
	}
	if rec := d.uploadPhoto(me, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{
		"activity_id": walk, "route_at": "2026-05-01T10:00:30Z", "caption": strings.Repeat("x", maxPhotoCaptionLen+1),
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("long uploaded caption: %d, want 400", rec.Code)
	}
}

// A PATCH refused for its route_at changes nothing, its caption included.
func TestPhotoUpdateRefusedLeavesCaptionAlone(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me := d.newAccount(false)
	start := photoTrackStart
	act := d.newActivity(me, testActivity{activityType: "walk", startedAt: start, durationSecs: 60, at: &[2]float64{10, 50}})
	var photo struct {
		ID string `json:"id"`
	}
	d.decode(d.uploadPhoto(me, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{"activity_id": act, "taken_at": "2026-05-01T10:00:00Z"}), http.StatusCreated, &photo)

	rec := d.do(me, http.MethodPatch, "/v1/photos/"+photo.ID, map[string]any{"caption": "new", "route_at": "not a time"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	var caption *string
	if err := d.pool.QueryRow(context.Background(), `SELECT caption FROM activity_photos WHERE id = $1`, photo.ID).Scan(&caption); err != nil {
		t.Fatal(err)
	}
	if caption != nil {
		t.Fatalf("caption = %q, want unchanged (none)", *caption)
	}
}

// Uploads racing each other for the last free place stop at the limit: each counts again under
// a per-account lock before inserting.
func TestPhotoLimitHoldsUnderConcurrency(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	act := d.newActivity(me, testActivity{activityType: "walk", startedAt: photoTrackStart, durationSecs: 60, at: &[2]float64{10, 50}})
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO activity_photos (user_id, activity_id, route_at, content_type, thumb_content_type, width, height, bytes, image_key)
		SELECT $1::uuid, $2, $3, 'image/jpeg', 'image/jpeg', 40, 30, 1, 'photos/' || $1::uuid::text || '/' || gen_random_uuid() FROM generate_series(1, $4)
	`, me.id, act, photoTrackStart, maxPhotosPerAccount-1); err != nil {
		t.Fatal(err)
	}
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)
	// Thirty, not a handful: with five the requests rarely overlapped enough to race at all.
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": act, "taken_at": "2026-05-01T10:00:00Z"})
		}()
	}
	wg.Wait()
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM activity_photos WHERE user_id = $1`, me.id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != maxPhotosPerAccount {
		t.Fatalf("%d photos, want %d", n, maxPhotosPerAccount)
	}
}

// sharePhoto gives owner a row over p's image files, as a copied Story's photo is (§4.23).
func (d *dbTest) sharePhoto(owner account, activityID string, p photoJSON) {
	d.t.Helper()
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO activity_photos (user_id, activity_id, route_at, content_type, thumb_content_type, width, height, bytes, image_key)
		SELECT $1, $2, route_at, content_type, thumb_content_type, width, height, bytes, image_key
		FROM activity_photos WHERE id = $3`, owner.id, activityID, p.ID); err != nil {
		d.t.Fatal(err)
	}
}

func TestSharedPhotoFilesGoWithTheirLastRow(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	me, other := d.newAccount(false), d.newAccount(false)
	file, thumb := testJPEG(t, 40, 30), testJPEG(t, 8, 6)
	walk := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	theirs := d.newActivity(other, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	var p photoJSON
	d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": walk, "taken_at": "2026-05-01T10:00:30Z"}), http.StatusCreated, &p)
	d.sharePhoto(other, theirs, p)
	key := photoKey(me.id, p.ID)
	stored := func() bool { return s3.Has(key) && s3.Has(photoThumbKey(key)) }

	// The other account's row is served from the same files.
	var list photosResponse
	d.decode(d.do(other, "GET", "/v1/photos?activity="+theirs, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 {
		t.Fatalf("shared row: %+v", list.Photos)
	}
	if rec := d.do(other, "GET", list.Photos[0].ThumbURL, nil); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("shared thumbnail: %d", rec.Code)
	}

	// Deleting the first row, or its whole activity, leaves the files to the other row.
	d.decode(d.do(me, "DELETE", p.URL, nil), http.StatusNoContent, nil)
	if !stored() {
		t.Fatalf("files removed while another row still uses them")
	}
	d.decode(d.uploadPhoto(me, file, thumb, map[string]string{"activity_id": walk, "taken_at": "2026-05-01T10:00:30Z"}), http.StatusCreated, &p)
	d.sharePhoto(other, theirs, p)
	second := photoKey(me.id, p.ID)
	d.decode(d.do(me, "DELETE", "/v1/activities/"+walk, nil), http.StatusNoContent, nil)
	if !s3.Has(second) {
		t.Fatalf("activity delete removed files another row still uses")
	}

	// The last row takes them.
	d.decode(d.do(other, "DELETE", "/v1/activities/"+theirs, nil), http.StatusNoContent, nil)
	if stored() || s3.Has(second) || s3.Has(photoThumbKey(second)) {
		t.Errorf("files left behind after their last row")
	}
}
