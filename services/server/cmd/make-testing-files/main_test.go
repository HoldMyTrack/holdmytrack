package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

const committed = "internal/web/static/testing"

// TestCommittedFilesAreGenerated regenerates every file and compares it with the committed
// copy, so a change to the generator or its inputs can't land without the files it writes.
func TestCommittedFilesAreGenerated(t *testing.T) {
	t.Chdir("../..")
	outDir, verbose = t.TempDir(), false
	generate()

	want, err := os.ReadDir(committed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("generated %d files, %d committed; run go run ./cmd/make-testing-files", len(got), len(want))
	}
	for _, e := range want {
		a, err := os.ReadFile(filepath.Join(committed, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			t.Fatalf("%s: committed but not generated", e.Name())
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from what the generator writes; run go run ./cmd/make-testing-files", e.Name())
		}
	}
}

// TestFilesDoWhatThePageSays parses each committed file the way ingest does, and checks it
// fails, or doesn't, the way /testing tells testers to expect.
func TestFilesDoWhatThePageSays(t *testing.T) {
	t.Chdir("../..")
	parseFile := func(name string) (parse.Activity, error) {
		f, err := os.Open(filepath.Join(committed, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		return parse.ByExtension(name, f)
	}
	timed := func(a parse.Activity) int {
		n := 0
		for _, p := range a.Points {
			if !p.Time.IsZero() {
				n++
			}
		}
		return n
	}

	for _, name := range []string{"west-side.gpx", "evening-loop.tcx", "mill-creek-falls.fit", "duplicate.tcx"} {
		a, err := parseFile(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if timed(a) < 2 {
			t.Errorf("%s: %d timed points, want a track", name, timed(a))
		}
	}
	// The FIT file carries every point of its source, and says it's a ride.
	fitAct, _ := parseFile("mill-creek-falls.fit")
	if src := load(millCreek); len(fitAct.Points) != len(src.Points) || fitAct.ActivityType != "cycling" {
		t.Errorf("mill-creek-falls.fit: %d points of type %q, want %d of cycling", len(fitAct.Points), fitAct.ActivityType, len(src.Points))
	}
	// The duplicate is the same outing as a track in the sample zip, without elevation.
	dup, _ := parseFile("duplicate.tcx")
	src := load(clague)
	if !dup.Points[0].Time.Equal(src.Points[0].Time) || dup.Points[0].Elevation != nil {
		t.Errorf("duplicate.tcx: starts %v with elevation %v, want %v and none", dup.Points[0].Time, dup.Points[0].Elevation, src.Points[0].Time)
	}

	if _, err := parseFile("broken.gpx"); err == nil || errors.Is(err, parse.ErrNoPoints) {
		t.Errorf("broken.gpx: %v, want a parse error", err)
	}
	if a, err := parseFile("planned-route.gpx"); err != nil || len(a.Points) < 2 || timed(a) != 0 {
		t.Errorf("planned-route.gpx: %v, %d points, %d timed; want points with no times", err, len(a.Points), timed(a))
	}
	if _, err := parseFile("indoor.gpx"); !errors.Is(err, parse.ErrNoPoints) {
		t.Errorf("indoor.gpx: %v, want ErrNoPoints", err)
	}
	if a, err := parseFile("one-point.gpx"); err != nil || timed(a) != 1 {
		t.Errorf("one-point.gpx: %v, %d timed points, want 1", err, timed(a))
	}
	if _, err := parseFile("track.kml"); !errors.Is(err, parse.ErrUnsupportedFormat) {
		t.Errorf("track.kml: %v, want ErrUnsupportedFormat", err)
	}
	if fi, err := os.Stat(filepath.Join(committed, "empty.gpx")); err != nil || fi.Size() != 0 {
		t.Errorf("empty.gpx: %v, want an empty file", err)
	}

	zr, err := zip.OpenReader(filepath.Join(committed, "holdmytrack-sample.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != len(sampleZip) {
		t.Errorf("holdmytrack-sample.zip: %d entries, want %d", len(zr.File), len(sampleZip))
	}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parse.ByExtension(f.Name, r); err != nil {
			t.Errorf("holdmytrack-sample.zip: %s: %v", f.Name, err)
		}
		r.Close()
	}

}
