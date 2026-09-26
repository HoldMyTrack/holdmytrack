package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// ExportDemoActivities writes the listed activities into outDir as demo_data/-ready GPX files,
// and outDir/manifest.json with their names, types and descriptions — the
// `export-demo-activities` CLI subcommand, run against a live deployment to pick real
// activities for the Demo Customer's history (SeedDemoCustomer). Each file holds the points
// the owner sees on the map (ingest.DisplayedPoints: clipped by their Private locations, with
// their track edit applied), never the original upload: demo_data/ is committed to a public
// repository, so what the owner's zones and edits removed must not be in it at all, not just
// hidden behind the demo account's own.
//
// Files are named "<YYYY-MM-DD> <slug>.gpx" from the start date in UTC and the activity's name
// (or type). A file already in outDir is never overwritten; the name gets a -2, -3, ... suffix
// instead, so several exports into one directory accumulate. Any failure stops the export,
// naming the activity id.
func ExportDemoActivities(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, outDir string, activityIDs []string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(outDir, demoManifestFile)
	manifest := map[string]demoManifestEntry{}
	if b, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(b, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", manifestPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var written []string
	for _, id := range activityIDs {
		filename, entry, err := exportDemoActivity(ctx, pool, store, outDir, id)
		if err != nil {
			return written, fmt.Errorf("activity %s: %w", id, err)
		}
		manifest[filename] = entry
		written = append(written, filename)

		// Rewritten after every file, so a failure part-way leaves a manifest that matches
		// the files already copied.
		b, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(manifestPath, append(b, '\n'), 0o644); err != nil {
			return written, err
		}
	}
	return written, nil
}

func exportDemoActivity(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, outDir, activityID string) (string, demoManifestEntry, error) {
	var (
		activityType string
		name         *string
		description  *string
	)
	err := pool.QueryRow(ctx,
		`SELECT activity_type, name, description FROM activities WHERE id = $1`, activityID,
	).Scan(&activityType, &name, &description)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", demoManifestEntry{}, errors.New("not found")
	}
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	points, err := ingest.DisplayedPoints(ctx, pool, store, activityID)
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	if points == nil {
		return "", demoManifestEntry{}, errors.New("no visible track: hidden entirely by Private locations")
	}

	entry := demoManifestEntry{Type: activityType}
	label := activityType
	if name != nil && *name != "" {
		entry.Name = *name
		label = *name
	}
	if description != nil {
		entry.Description = *description
	}
	filename, err := freeDemoFilename(outDir, points[0].Time.UTC().Format("2006-01-02")+" "+demoSlug(label), ".gpx")
	if err != nil {
		return "", demoManifestEntry{}, err
	}

	f, err := os.OpenFile(filepath.Join(outDir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	if err := writeDemoGPX(f, activityType, points); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", demoManifestEntry{}, err
	}
	if err := f.Close(); err != nil {
		return "", demoManifestEntry{}, err
	}
	return filename, entry, nil
}

// writeDemoGPX writes points as a one-segment GPX 1.1 track with everything parse.ParseGPX
// reads back — position, elevation, time and heart rate — and nothing else: no creator
// device, no author metadata. Coordinates and elevation keep their full precision, so a
// re-ingest derives the same metrics and trajectory; the name lives in the manifest.
func writeDemoGPX(w io.Writer, activityType string, points []parse.Point) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="HoldMyTrack" xmlns="http://www.topografix.com/GPX/1/1" xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">` + "\n")
	b.WriteString("  <trk>\n    <type>")
	xml.EscapeText(&b, []byte(activityType))
	b.WriteString("</type>\n    <trkseg>\n")
	for _, p := range points {
		fmt.Fprintf(&b, `      <trkpt lat="%s" lon="%s">`,
			strconv.FormatFloat(p.Lat, 'f', -1, 64), strconv.FormatFloat(p.Lon, 'f', -1, 64))
		if p.Elevation != nil {
			fmt.Fprintf(&b, "<ele>%s</ele>", strconv.FormatFloat(float64(*p.Elevation), 'f', -1, 32))
		}
		fmt.Fprintf(&b, "<time>%s</time>", p.Time.UTC().Format(time.RFC3339Nano))
		if p.HeartRate != nil {
			fmt.Fprintf(&b, "<extensions><gpxtpx:TrackPointExtension><gpxtpx:hr>%d</gpxtpx:hr></gpxtpx:TrackPointExtension></extensions>", *p.HeartRate)
		}
		b.WriteString("</trkpt>\n")
	}
	b.WriteString("    </trkseg>\n  </trk>\n</gpx>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

var demoSlugUnsafe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// demoSlug keeps a name readable as a filename: letters and digits, everything else collapsed
// to single spaces, bounded in length.
func demoSlug(s string) string {
	s = strings.TrimSpace(demoSlugUnsafe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 60 {
		s = strings.TrimSpace(string(r[:60]))
	}
	if s == "" {
		return "Activity"
	}
	return s
}

// freeDemoFilename returns base+ext, or base-2+ext, base-3+ext, ... — the first not already in
// dir. The manifest keys files by name, so two activities must never share one.
func freeDemoFilename(dir, base, ext string) (string, error) {
	for n := 1; ; n++ {
		name := base + ext
		if n > 1 {
			name = fmt.Sprintf("%s-%d%s", base, n, ext)
		}
		_, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
	}
}
