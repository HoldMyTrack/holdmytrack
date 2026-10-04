package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// The committed demo_data/ must always seed: every file parseable, every manifest entry
// pointing at a real file. An empty history is allowed — manifest.json alone keeps the
// directory embeddable while the real set is being picked.
func TestEmbeddedDemoDataIsSeedable(t *testing.T) {
	fsys, err := fs.Sub(demoData, "demo_data")
	if err != nil {
		t.Fatal(err)
	}
	names, err := demoFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := loadDemoManifest(fsys, names)
	if err != nil {
		t.Fatal(err)
	}
	// The Stories a demo visitor sees (docs/SPEC.md FR-2.2).
	if len(manifest.Stories) != 4 || manifest.Stories[0].Name != "Brecksville Reservation trip" || len(manifest.Stories[0].Activities) != 3 ||
		manifest.Stories[1].Name != "Emerald Necklace Trail" || len(manifest.Stories[1].Activities) != 3 ||
		manifest.Stories[2].Name != "Greater Cleveland trails" || len(manifest.Stories[2].Activities) != 4 ||
		manifest.Stories[3].Name != "Niagara Falls trip" || len(manifest.Stories[3].Activities) != 6 {
		t.Errorf("demo stories %+v", manifest.Stories)
	}
}

func TestDemoFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"b.fit":         {},
		"a.gpx":         {},
		"c.JSON":        {},
		"manifest.json": {},
	}
	got, err := demoFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.gpx", "b.fit", "c.JSON"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	fsys["notes.txt"] = &fstest.MapFile{}
	if _, err := demoFiles(fsys); err == nil {
		t.Fatal("want an error for an unsupported file")
	}
}

func TestLoadDemoManifest(t *testing.T) {
	files := []string{"a.gpx", "b.fit"}

	got, err := loadDemoManifest(fstest.MapFS{}, files)
	if err != nil || len(got.Activities) != 0 || len(got.Stories) != 0 {
		t.Fatalf("missing manifest: got %+v, %v; want empty, nil", got, err)
	}

	fsys := fstest.MapFS{"manifest.json": {Data: []byte(`{
		"activities": {"a.gpx": {"name": "Lake loop", "type": "walking"}, "b.fit": {"description": "Along the river"}},
		"stories": [{"name": "Weekend", "description": "Two days out", "activities": ["a.gpx", "b.fit"]}]
	}`)}}
	got, err = loadDemoManifest(fsys, files)
	if err != nil {
		t.Fatal(err)
	}
	if want := (demoManifestEntry{Name: "Lake loop", Type: "walking"}); !reflect.DeepEqual(got.Activities["a.gpx"], want) {
		t.Fatalf("got %+v, want %+v", got.Activities["a.gpx"], want)
	}
	if want := (demoManifestEntry{Description: "Along the river"}); !reflect.DeepEqual(got.Activities["b.fit"], want) {
		t.Fatalf("got %+v, want %+v", got.Activities["b.fit"], want)
	}
	if want := []demoManifestStory{{Name: "Weekend", Description: "Two days out", Activities: []string{"a.gpx", "b.fit"}}}; !reflect.DeepEqual(got.Stories, want) {
		t.Fatalf("stories %+v, want %+v", got.Stories, want)
	}

	for name, manifest := range map[string]string{
		"an entry with no matching file":    `{"activities": {"gone.gpx": {"name": "x"}}}`,
		"a story naming a missing file":     `{"stories": [{"name": "S", "activities": ["gone.gpx"]}]}`,
		"a story with no name":              `{"stories": [{"name": " ", "activities": ["a.gpx"]}]}`,
		"a story with no activities":        `{"stories": [{"name": "S", "activities": []}]}`,
		"two stories with one name":         `{"stories": [{"name": "S", "activities": ["a.gpx"]}, {"name": "S", "activities": ["b.fit"]}]}`,
		"a story name over the API's limit": `{"stories": [{"name": "` + strings.Repeat("x", maxStoryNameLen+1) + `", "activities": ["a.gpx"]}]}`,
	} {
		fsys["manifest.json"] = &fstest.MapFile{Data: []byte(manifest)}
		if _, err := loadDemoManifest(fsys, files); err == nil {
			t.Errorf("want an error for %s", name)
		}
	}
}

func TestLoadDemoManifestPhotos(t *testing.T) {
	files := []string{"a.gpx"}
	fsys := fstest.MapFS{"photos/a 1.jpg": {}, "photos/a 1-thumb.jpg": {}}
	photo := func(fields string) string {
		return `{"activities": {"a.gpx": {"photos": [{` + fields + `}]}}}`
	}
	ok := `"file": "a 1.jpg", "thumb": "a 1-thumb.jpg", "route_at": "2026-10-02T15:04:05Z", "width": 2048, "height": 1536`

	fsys["manifest.json"] = &fstest.MapFile{Data: []byte(photo(ok + `, "caption": "Lake view"`))}
	got, err := loadDemoManifest(fsys, files)
	if err != nil {
		t.Fatal(err)
	}
	want := []demoManifestPhoto{{File: "a 1.jpg", Thumb: "a 1-thumb.jpg", RouteAt: time.Date(2026, 10, 2, 15, 4, 5, 0, time.UTC), Caption: "Lake view", Width: 2048, Height: 1536}}
	if !reflect.DeepEqual(got.Activities["a.gpx"].Photos, want) {
		t.Fatalf("photos %+v, want %+v", got.Activities["a.gpx"].Photos, want)
	}

	for name, manifest := range map[string]string{
		"a missing image":       photo(`"file": "gone.jpg", "thumb": "a 1-thumb.jpg", "route_at": "2026-10-02T15:04:05Z", "width": 1, "height": 1`),
		"an unsupported format": photo(`"file": "a 1.png", "thumb": "a 1-thumb.jpg", "route_at": "2026-10-02T15:04:05Z", "width": 1, "height": 1`),
		"no route_at":           photo(`"file": "a 1.jpg", "thumb": "a 1-thumb.jpg", "width": 1, "height": 1`),
		"no size":               photo(`"file": "a 1.jpg", "thumb": "a 1-thumb.jpg", "route_at": "2026-10-02T15:04:05Z"`),
		"one file twice":        photo(`"file": "a 1.jpg", "thumb": "a 1.jpg", "route_at": "2026-10-02T15:04:05Z", "width": 1, "height": 1`),
		"a caption over limit":  photo(ok + `, "caption": "` + strings.Repeat("x", maxPhotoCaptionLen+1) + `"`),
	} {
		fsys["manifest.json"] = &fstest.MapFile{Data: []byte(manifest)}
		if _, err := loadDemoManifest(fsys, files); err == nil {
			t.Errorf("want an error for %s", name)
		}
	}
}

func TestFreeDemoFilename(t *testing.T) {
	dir := t.TempDir()
	for _, want := range []string{"2026-09-01 Walk.gpx", "2026-09-01 Walk-2.gpx", "2026-09-01 Walk-3.gpx"} {
		got, err := freeDemoFilename(dir, "2026-09-01 Walk", ".gpx")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if err := os.WriteFile(filepath.Join(dir, got), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A photo through export-demo-activities and back in through the seed (exportDemoPhotos,
// seedDemoPhotos): its images written out and stored again under the new owner, its place,
// caption and capture time unchanged, and a re-run of the seed leaving one photo, not two.
func TestDemoPhotosRoundTrip(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	ctx := context.Background()
	owner, demo := d.newAccount(false), d.newAccount(false)
	walk := d.newActivity(owner, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	var uploaded photoJSON
	d.decode(d.uploadPhoto(owner, testJPEG(t, 400, 300), testJPEG(t, 32, 24), map[string]string{
		"activity_id": walk, "taken_at": "2026-05-01T10:00:30Z", "caption": "The bridge",
	}), http.StatusCreated, &uploaded)

	out := t.TempDir()
	points := []parse.Point{{Time: photoTrackStart}, {Time: photoTrackStart.Add(time.Minute)}}
	photos, err := exportDemoPhotos(ctx, d.pool, d.srv.store, out, walk, "2026-05-01 Walk", points)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 1 || photos[0].File != "2026-05-01 Walk 1.jpg" || photos[0].Thumb != "2026-05-01 Walk 1-thumb.jpg" ||
		photos[0].Caption != "The bridge" || photos[0].Width != 400 || photos[0].Height != 300 ||
		!photos[0].RouteAt.Equal(photoTrackStart.Add(30*time.Second)) || photos[0].TakenAt == nil {
		t.Fatalf("exported %+v", photos)
	}
	for _, f := range []string{photos[0].File, photos[0].Thumb} {
		if _, err := os.Stat(filepath.Join(out, demoPhotoDir, f)); err != nil {
			t.Errorf("exported image: %v", err)
		}
	}

	seeded := d.newActivity(demo, testActivity{activityType: "walking", durationSecs: 60, startedAt: photoTrackStart, at: &[2]float64{10, 50}})
	for run := 1; run <= 2; run++ {
		if err := seedDemoPhotos(ctx, d.pool, d.srv.store, os.DirFS(out), demo.id, seeded, photos); err != nil {
			t.Fatalf("seed run %d: %v", run, err)
		}
	}
	var list photosResponse
	d.decode(d.do(demo, "GET", "/v1/photos?activity="+seeded, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 {
		t.Fatalf("seeded twice: %d photos, want 1", len(list.Photos))
	}
	p := list.Photos[0]
	if !near(p.Lon, 10.0005) || !near(p.Lat, 50.0005) || p.Caption == nil || *p.Caption != "The bridge" || p.TakenAt == nil {
		t.Errorf("seeded photo %+v", p)
	}
	if !s3.Has(photoKey(demo.id, p.ID)) || !s3.Has(photoThumbKey(demo.id, p.ID)) {
		t.Errorf("seeded images not stored under the new owner")
	}
}
