package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

const testGPX = `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="test" xmlns="http://www.topografix.com/GPX/1/1"><trk><type>walking</type><trkseg>
<trkpt lat="50.0000" lon="10.0000"><time>2026-05-01T10:00:00Z</time></trkpt>
<trkpt lat="50.0010" lon="10.0010"><time>2026-05-01T10:00:30Z</time></trkpt>
<trkpt lat="50.0020" lon="10.0020"><time>2026-05-01T10:01:00Z</time></trkpt>
</trkseg></trk></gpx>`

// Reads TEST_DATABASE_URL and skips without it, like internal/httpapi's database tests.
func TestBuildWritesEverythingAcrossParts(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s3 := storagetest.New()
	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	var userID string
	email := fmt.Sprintf("export-test-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, timezone, display_name) VALUES ($1, 'Europe/Berlin', 'Ann') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })

	rawKey := "raw/" + userID + "/abc.gpx"
	s3.Put(rawKey, []byte(testGPX))
	var walk, copyOf string
	started := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if err := pool.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, source_detail, activity_type, name, started_at, raw_payload_key, distance_meters, timezone)
		VALUES ($1, 'upload', 'walk.gpx', 'walking', 'Lake / Loop', $2, $3, 260, 'UTC') RETURNING id`, userID, started, rawKey).Scan(&walk); err != nil {
		t.Fatal(err)
	}
	// A second activity on the same raw file (a cross-source duplicate): the original is
	// written once, and both point at it.
	if err := pool.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, source_detail, activity_type, name, started_at, raw_payload_key, superseded_by, timezone)
		VALUES ($1, 'takeout', 'walk.gpx', 'walking', 'Lake / Loop', $2, $3, $4, 'UTC') RETURNING id`, userID, started.Add(time.Minute), rawKey, walk).Scan(&copyOf); err != nil {
		t.Fatal(err)
	}
	var photoID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO activity_photos (user_id, activity_id, route_at, width, height, bytes, caption, content_type, thumb_content_type)
		VALUES ($1, $2, $3, 10, 10, 3, 'The bridge', 'image/jpeg', 'image/jpeg') RETURNING id`, userID, walk, started).Scan(&photoID); err != nil {
		t.Fatal(err)
	}
	s3.Put("photos/"+userID+"/"+photoID, []byte("jpg"))
	if _, err := pool.Exec(ctx, `INSERT INTO privacy_zones (user_id, center, radius_m, name) VALUES ($1, ST_MakePoint(13.4, 52.5)::geography, 300, 'Home')`, userID); err != nil {
		t.Fatal(err)
	}
	var storyID string
	if err := pool.QueryRow(ctx, `INSERT INTO stories (user_id, name) VALUES ($1, 'Spring') RETURNING id`, userID).Scan(&storyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO story_activities (story_id, activity_id) VALUES ($1, $2)`, storyID, walk); err != nil {
		t.Fatal(err)
	}

	// Small parts, so the archive has to split.
	limit := PartLimit
	PartLimit = 100
	t.Cleanup(func() { PartLimit = limit })
	exportID := "00000000-0000-0000-0000-000000000001"
	sizes, err := Build(ctx, pool, store, userID, exportID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) < 2 {
		t.Fatalf("%d parts, want a split", len(sizes))
	}

	files := map[string][]byte{}
	for n := range sizes {
		b, ok := s3.Object(PartKey(userID, exportID, n+1))
		if !ok || int64(len(b)) != sizes[n] {
			t.Fatalf("part %d: stored %v, %d bytes, want %d", n+1, ok, len(b), sizes[n])
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatalf("part %d: %v", n+1, err)
		}
		for _, f := range zr.File {
			r, _ := f.Open()
			files[f.Name], _ = io.ReadAll(r)
			r.Close()
		}
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"README.txt", "account.json",
		"originals/2026-05-01 1200 Lake Loop.gpx",
		"photos/2026-05-01 1200 Lake Loop 1.jpg",
		"tracks/2026-05-01 1200 Lake Loop.gpx",
		"tracks/2026-05-01 1201 Lake Loop.gpx",
	}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s\nwant:\n%s", strings.Join(names, "\n"), strings.Join(want, "\n"))
	}
	if string(files["originals/2026-05-01 1200 Lake Loop.gpx"]) != testGPX {
		t.Error("the original isn't the uploaded file")
	}

	var doc struct {
		Account struct {
			DisplayName string `json:"display_name"`
		}
		Activities []struct {
			ID, Original, Track string
			DuplicateOf         string `json:"duplicate_of"`
			Photos              []struct{ File, Caption string }
		}
		Stories []struct {
			Name       string
			Activities []string
		}
		PrivateLocations []struct {
			Name    string
			RadiusM int `json:"radius_m"`
		} `json:"private_locations"`
	}
	if err := json.Unmarshal(files["account.json"], &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Account.DisplayName != "Ann" || len(doc.Activities) != 2 || len(doc.Stories) != 1 || doc.Stories[0].Activities[0] != walk ||
		len(doc.PrivateLocations) != 1 || doc.PrivateLocations[0].RadiusM != 300 {
		t.Fatalf("account.json: %+v (walk %s)", doc, walk)
	}
	for _, a := range doc.Activities {
		if a.Original != "originals/2026-05-01 1200 Lake Loop.gpx" {
			t.Errorf("activity %s: original %q", a.ID, a.Original)
		}
	}
	if doc.Activities[0].ID != walk || doc.Activities[1].DuplicateOf != walk || len(doc.Activities[0].Photos) != 1 || doc.Activities[0].Photos[0].Caption != "The bridge" {
		t.Errorf("activities: %+v", doc.Activities)
	}
	if !strings.Contains(string(files["README.txt"]), "originals/") {
		t.Error("README doesn't explain the folders")
	}

	// A retried build starts clean: the earlier attempt's parts don't linger past the new count.
	PartLimit = limit
	sizes, err = Build(ctx, pool, store, userID, exportID, "en")
	if err != nil || len(sizes) != 1 {
		t.Fatalf("rebuild: %d parts, %v", len(sizes), err)
	}
	if s3.Has(PartKey(userID, exportID, 2)) {
		t.Error("a part from the earlier build is still stored")
	}
}
