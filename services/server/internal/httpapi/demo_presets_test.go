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
	if len(manifest.Stories) != 8 || manifest.Stories[0].Name != "Brecksville Reservation trip" || len(manifest.Stories[0].Activities) != 3 ||
		manifest.Stories[1].Name != "Emerald Necklace Trail" || len(manifest.Stories[1].Activities) != 3 ||
		manifest.Stories[2].Name != "Greater Cleveland trails" || len(manifest.Stories[2].Activities) != 8 ||
		manifest.Stories[3].Name != "Niagara Falls trip" || len(manifest.Stories[3].Activities) != 6 ||
		manifest.Stories[4].Name != "Preston's H.O.P.E. Playground Park" || len(manifest.Stories[4].Activities) != 3 ||
		manifest.Stories[5].Name != "Italy" || len(manifest.Stories[5].Activities) != 8 ||
		manifest.Stories[6].Name != "Estonia" || len(manifest.Stories[6].Activities) != 3 ||
		manifest.Stories[7].Name != "Vietnam" || len(manifest.Stories[7].Activities) != 5 {
		t.Errorf("demo stories %+v", manifest.Stories)
	}
	if len(manifest.SpotCaptures) != 7 {
		t.Errorf("demo captures %+v", manifest.SpotCaptures)
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

func TestLoadDemoManifestCaptures(t *testing.T) {
	fsys := fstest.MapFS{}
	capture := func(fields ...string) string {
		return `{"spot_captures": [{` + strings.Join(fields, `}, {`) + `}]}`
	}
	ok := `"osm_type": "way", "osm_id": 631388618, "name": "Playground", "captured_at": "2026-10-03T15:13:30.193259Z"`

	fsys["manifest.json"] = &fstest.MapFile{Data: []byte(capture(ok))}
	got, err := loadDemoManifest(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []demoManifestCapture{{OSMType: "way", OSMID: 631388618, Name: "Playground", CapturedAt: time.Date(2026, 10, 3, 15, 13, 30, 193259000, time.UTC)}}
	if !reflect.DeepEqual(got.SpotCaptures, want) {
		t.Fatalf("captures %+v, want %+v", got.SpotCaptures, want)
	}

	for name, manifest := range map[string]string{
		"an unknown osm_type": capture(`"osm_type": "area", "osm_id": 1, "captured_at": "2026-10-03T15:13:30Z"`),
		"no osm_id":           capture(`"osm_type": "node", "captured_at": "2026-10-03T15:13:30Z"`),
		"no captured_at":      capture(`"osm_type": "node", "osm_id": 1`),
		"one place twice":     capture(ok, ok),
	} {
		fsys["manifest.json"] = &fstest.MapFile{Data: []byte(manifest)}
		if _, err := loadDemoManifest(fsys, nil); err == nil {
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

// Captures through export-demo-activities and back in through the seed (exportDemoCaptures,
// seedDemoCaptures): only the owner's within the span, none reaching into their Private
// locations, and a re-run of the seed keeping one capture per place at the manifest's time.
func TestDemoCapturesRoundTrip(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	owner, other, demo := d.newAccount(false), d.newAccount(false), d.newAccount(false)
	base := time.Now().UnixNano()
	// Three places mapped as points — 30 m circles — in open ocean no other test uses.
	spot := func(n int, lon float64) int64 {
		var id int64
		if err := d.pool.QueryRow(ctx, `
			INSERT INTO spots (category, name, geom, osm_type, osm_id)
			VALUES ('playground', 'Test Playground', ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($1, -41.5), 4326)::geography, 30)::geometry), 'node', $2)
			RETURNING id`, lon, base+int64(n)).Scan(&id); err != nil {
			t.Fatalf("create spot: %v", err)
		}
		t.Cleanup(func() { d.pool.Exec(context.Background(), `DELETE FROM spots WHERE id = $1`, id) })
		return id
	}
	inside, early, home := spot(1, -41.5), spot(2, -41.6), spot(3, -41.7)
	if _, err := d.pool.Exec(ctx, `INSERT INTO privacy_zones (user_id, center, radius_m) VALUES ($1, ST_SetSRID(ST_MakePoint(-41.7, -41.5005), 4326)::geography, 200)`, owner.id); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 3, 14, 50, 0, 0, time.UTC)
	span := demoSpan{start.UnixMilli(), start.Add(2 * time.Hour).UnixMilli()}
	for _, c := range []struct {
		user account
		spot int64
		at   time.Time
	}{
		{owner, inside, start.Add(time.Hour)},
		{owner, early, start.Add(-time.Minute)},
		{owner, home, start.Add(time.Hour)},
		{other, early, start.Add(time.Hour)},
	} {
		if _, err := d.pool.Exec(ctx, `INSERT INTO spot_captures (user_id, spot_id, captured_at) VALUES ($1, $2, $3)`, c.user.id, c.spot, c.at); err != nil {
			t.Fatal(err)
		}
	}

	captures, err := exportDemoCaptures(ctx, d.pool, owner.id, span)
	if err != nil {
		t.Fatal(err)
	}
	if len(captures) != 1 || captures[0].OSMType != "node" || captures[0].OSMID != base+1 || captures[0].Name != "Test Playground" ||
		!captures[0].CapturedAt.Equal(start.Add(time.Hour)) {
		t.Fatalf("exported %+v", captures)
	}

	gone := demoManifestCapture{OSMType: "way", OSMID: base, CapturedAt: start}
	for run := 1; run <= 2; run++ {
		missing, err := seedDemoCaptures(ctx, d.pool, demo.id, append(captures, gone))
		if err != nil {
			t.Fatalf("seed run %d: %v", run, err)
		}
		if len(missing) != 1 || missing[0] != gone {
			t.Fatalf("seed run %d: missing %+v, want the place not loaded", run, missing)
		}
	}
	var list spotCapturesResponse
	d.decode(d.do(demo, "GET", "/v1/spots/captures", nil), http.StatusOK, &list)
	if len(list.Captures) != 1 || list.Captures[0].SpotID != inside || !list.Captures[0].CapturedAt.Equal(start.Add(time.Hour)) {
		t.Errorf("seeded captures %+v", list.Captures)
	}
}
