package httpapi

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
	if _, err := loadDemoManifest(fsys, names); err != nil {
		t.Fatal(err)
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
	if err != nil || len(got) != 0 {
		t.Fatalf("missing manifest: got %v, %v; want empty, nil", got, err)
	}

	fsys := fstest.MapFS{"manifest.json": {Data: []byte(`{"a.gpx": {"name": "Lake loop", "type": "walking"}}`)}}
	got, err = loadDemoManifest(fsys, files)
	if err != nil {
		t.Fatal(err)
	}
	if want := (demoManifestEntry{Name: "Lake loop", Type: "walking"}); got["a.gpx"] != want {
		t.Fatalf("got %+v, want %+v", got["a.gpx"], want)
	}

	fsys["manifest.json"] = &fstest.MapFile{Data: []byte(`{"b.fit": {"description": "Along the river"}}`)}
	got, err = loadDemoManifest(fsys, files)
	if err != nil {
		t.Fatal(err)
	}
	if want := (demoManifestEntry{Description: "Along the river"}); got["b.fit"] != want {
		t.Fatalf("got %+v, want %+v", got["b.fit"], want)
	}

	fsys["manifest.json"] = &fstest.MapFile{Data: []byte(`{"gone.gpx": {"name": "x"}}`)}
	if _, err := loadDemoManifest(fsys, files); err == nil {
		t.Fatal("want an error for an entry with no matching file")
	}
}

func TestDemoSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Morning Run":         "Morning Run",
		"  Lake / Loop #2!  ": "Lake Loop 2",
		"Прогулка у озера":    "Прогулка у озера",
		"???":                 "Activity",
		"":                    "Activity",
	} {
		if got := demoSlug(in); got != want {
			t.Errorf("demoSlug(%q) = %q, want %q", in, got, want)
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

// The exported GPX must parse back to exactly the points it was written from, or the demo's
// re-ingest would drift from what the owner sees.
func TestWriteDemoGPXRoundTrip(t *testing.T) {
	ele := float32(187.4)
	hr := int16(131)
	t0 := time.Date(2026, 9, 26, 14, 3, 7, 250_000_000, time.UTC)
	points := []parse.Point{
		{Lat: 41.3213457891234, Lon: -81.6123456789012, Elevation: &ele, Time: t0, HeartRate: &hr},
		{Lat: 41.32141, Lon: -81.61229, Time: t0.Add(time.Second)},
	}
	var buf bytes.Buffer
	if err := writeDemoGPX(&buf, "hiking & walking", points); err != nil {
		t.Fatal(err)
	}
	act, err := parse.ParseGPX(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if act.ActivityType != "hiking & walking" {
		t.Errorf("type = %q", act.ActivityType)
	}
	if !reflect.DeepEqual(act.Points, points) {
		t.Fatalf("got %+v, want %+v", act.Points, points)
	}
}
