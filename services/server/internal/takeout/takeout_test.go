package takeout

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// testdata/sample is pathify's own synthetic export (tests/fixtures/takeout-sample, MIT),
// laid out like a real one: two devices interleaved in one day file, three outings in one
// day, a six-minute hole inside a walk, a ride across midnight UTC, a swim whose day file is
// missing, and a menstrual-health file that must never be read. testdata/pathify-out is what
// pathify 1.4.0's `takeout --type <T> --per-activity` wrote for it.

// zipDir builds the in-memory zip an upload would arrive as.
func zipDir(t *testing.T, root string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
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
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// assertMatchesPathify extracts every GPS-bearing type and compares each file, name and
// bytes, to what pathify wrote into want/<type>/.
func assertMatchesPathify(t *testing.T, zr *zip.Reader, want string) {
	t.Helper()
	archive, err := Open(zr)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range archive.Types() {
		if typ.WithGPS == 0 {
			continue
		}
		activities, err := archive.Extract(typ.Name)
		if err != nil {
			t.Fatalf("%s: %v", typ.Name, err)
		}
		got := make(map[string][]byte)
		for i, name := range FileNames(activities) {
			got[name] = activities[i].GPX()
		}

		entries, _ := os.ReadDir(filepath.Join(want, typ.Name))
		var wantNames []string
		for _, e := range entries {
			wantNames = append(wantNames, e.Name())
		}
		var gotNames []string
		for name := range got {
			gotNames = append(gotNames, name)
		}
		sort.Strings(gotNames)
		if strings.Join(gotNames, "\n") != strings.Join(wantNames, "\n") {
			t.Errorf("%s: files\n got %v\nwant %v", typ.Name, gotNames, wantNames)
			continue
		}
		for _, name := range wantNames {
			wantBytes, err := os.ReadFile(filepath.Join(want, typ.Name, name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got[name], wantBytes) {
				t.Errorf("%s/%s differs from pathify's output\n got:\n%s\nwant:\n%s", typ.Name, name, got[name], wantBytes)
			}
		}
	}
}

func TestSampleMatchesPathify(t *testing.T) {
	assertMatchesPathify(t, zipDir(t, "testdata/sample"), "testdata/pathify-out")
}

// TestRealExportMatchesPathify runs the same comparison against a real export, which can't
// be committed (it's someone's health data). Point TAKEOUT_ARCHIVE at the zip and
// TAKEOUT_PATHIFY_OUT at a directory holding `pathify takeout <zip> --type <T>
// --per-activity -o <dir>/<T>` for each type with GPS.
func TestRealExportMatchesPathify(t *testing.T) {
	archivePath, want := os.Getenv("TAKEOUT_ARCHIVE"), os.Getenv("TAKEOUT_PATHIFY_OUT")
	if archivePath == "" || want == "" {
		t.Skip("TAKEOUT_ARCHIVE and TAKEOUT_PATHIFY_OUT not set")
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	assertMatchesPathify(t, &zr.Reader, want)
}

func TestSampleJoin(t *testing.T) {
	archive, err := Open(zipDir(t, "testdata/sample"))
	if err != nil {
		t.Fatal(err)
	}

	var types []string
	for _, typ := range archive.Types() {
		types = append(types, typ.Name)
	}
	if got, want := strings.Join(types, ","), "Walk,Outdoor Bike,Swim,Workout"; got != want {
		t.Errorf("types = %s, want %s", got, want)
	}

	walks, err := archive.Extract("walk") // matched case-insensitively
	if err != nil {
		t.Fatal(err)
	}
	if len(walks) != 1 {
		t.Fatalf("walks = %d, want 1 (log 1002 has no GPS)", len(walks))
	}
	walk := walks[0]
	if walk.Source != "Pathify Phone" {
		t.Errorf("kept source = %q; the device with the most fixes should win", walk.Source)
	}
	if len(walk.Segments) != 2 {
		t.Errorf("segments = %d, want 2: the six-minute hole starts a new one", len(walk.Segments))
	}
	for _, segment := range walk.Segments {
		for _, p := range segment {
			if p.source != walk.Source {
				t.Fatalf("a fix from %q leaked into a track kept from %q", p.source, walk.Source)
			}
		}
	}

	rides, err := archive.Extract("Outdoor Bike")
	if err != nil {
		t.Fatal(err)
	}
	last := rides[0].Segments[len(rides[0].Segments)-1]
	if day := last[len(last)-1].time.Format(time.DateOnly); day != "2026-07-12" {
		t.Errorf("ride ends on %s; the window crosses midnight UTC and must read the next day file", day)
	}

	swims, err := archive.Extract("Swim")
	if err != nil {
		t.Fatal(err)
	}
	if len(swims) != 0 {
		t.Errorf("swims = %d, want 0: its day file is missing", len(swims))
	}

	for _, a := range append(walks, rides...) {
		if bytes.Contains(a.GPX(), []byte("SENSITIVE")) {
			t.Fatal("menstrual-health data reached the output")
		}
	}
}

func TestChooseSourceTieGoesToTheLastDeviceSeen(t *testing.T) {
	fixes := []fix{{source: "Phone"}, {source: "Watch"}, {source: "Phone"}, {source: "Watch"}}
	if got := chooseSource(fixes); got != "Watch" {
		t.Errorf("chooseSource = %q, want Watch (pathify's max_by_key keeps the last maximum)", got)
	}
}

func TestDetectOffsetFindsLocalWallClock(t *testing.T) {
	// Logs written in UTC-4 wall-clock time over fixes recorded in real UTC.
	var fixes []fix
	var logs []Log
	for d := 0; d < 3; d++ {
		utcStart := time.Date(2026, 7, 10+d, 14, 0, 0, 0, time.UTC)
		for m := 0; m < 30; m++ {
			fixes = append(fixes, fix{time: utcStart.Add(time.Duration(m) * time.Minute), source: "Phone"})
		}
		logs = append(logs, Log{ID: int64(d), Start: utcStart.Add(-4 * time.Hour), Duration: 30 * time.Minute, HasGPS: true})
	}
	cache := dayCache{archive: &Archive{}, days: map[string][]fix{}}
	for _, f := range fixes {
		day := f.time.Format(time.DateOnly)
		cache.days[day] = append(cache.days[day], f)
	}
	for _, day := range []string{"2026-07-09", "2026-07-13"} {
		cache.days[day] = nil
	}
	offset, err := detectOffset(logs, &cache)
	if err != nil {
		t.Fatal(err)
	}
	if offset != -4*time.Hour {
		t.Errorf("offset = %v, want -4h", offset)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Walk": "walk", "Outdoor Bike": "outdoor-bike", "Rowing  machine": "rowing-machine",
		"Sport/Other": "sport-other", "///": "activity",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLon(t *testing.T) {
	for in, want := range map[float64]float64{-81.7433: -81.7433, 190: -170, -190: 170, 180: 180, -180: -180} {
		if got := normalizeLon(in); got != want {
			t.Errorf("normalizeLon(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestGPXFloatMatchesRustDisplay(t *testing.T) {
	for in, want := range map[float64]string{
		236:                      "236",
		41.3931:                  "41.3931",
		-81.743388:               "-81.743388",
		0.0000001:                "0.0000001",
		float64(float32(237.6)):  "237.60000610351563", // exact tie: Rust rounds up, Go to even
		float64(float32(-237.6)): "-237.60000610351563",
		236.20001220703125:       "236.20001220703125",
	} {
		if got := gpxFloat(in); got != want {
			t.Errorf("gpxFloat(%v) = %s, want %s", in, got, want)
		}
	}
}
