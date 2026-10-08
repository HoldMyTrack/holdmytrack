package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// ExportDemoActivities writes the listed activities into outDir as demo_data/-ready GPX files,
// their photos' images into outDir/photos/, and outDir/manifest.json with their names, types,
// descriptions and photos, and the owner's Spots captures over the same span — the
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
//
// Captures belong to the account, not to an activity, so they're picked by time: every one the
// owner made from the first exported point to the last (exportDemoCaptures). That takes in a
// place captured between two of a trip's activities, so a trip is exported in one run.
func ExportDemoActivities(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, outDir string, activityIDs []string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(outDir, demoManifestFile)
	// An existing manifest's Stories are kept as they are; only activity entries are added.
	manifest := demoManifest{Activities: map[string]demoManifestEntry{}}
	if b, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(b, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", manifestPath, err)
		}
		if manifest.Activities == nil {
			manifest.Activities = map[string]demoManifestEntry{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	writeManifest := func() error {
		b, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(manifestPath, append(b, '\n'), 0o644)
	}
	var written []string
	spans := map[string]demoSpan{}
	for _, id := range activityIDs {
		filename, entry, owner, span, err := exportDemoActivity(ctx, pool, store, outDir, id)
		if err != nil {
			return written, fmt.Errorf("activity %s: %w", id, err)
		}
		manifest.Activities[filename] = entry
		written = append(written, filename)
		if s, ok := spans[owner]; ok {
			span = demoSpan{min(s.start, span.start), max(s.end, span.end)}
		}
		spans[owner] = span

		// Rewritten after every file, so a failure part-way leaves a manifest that matches
		// the files already copied.
		if err := writeManifest(); err != nil {
			return written, err
		}
	}
	for owner, span := range spans {
		captures, err := exportDemoCaptures(ctx, pool, owner, span)
		if err != nil {
			return written, fmt.Errorf("captures: %w", err)
		}
		manifest.SpotCaptures = mergeDemoCaptures(manifest.SpotCaptures, captures)
	}
	return written, writeManifest()
}

// demoSpan is the time from an export's first point to its last, as Unix milliseconds so the
// built-in min and max apply.
type demoSpan struct{ start, end int64 }

// exportDemoCaptures returns the owner's captures within span, oldest first, except a place
// that reaches into one of the owner's Private locations: a playground by their home would
// give away where they live, and demo_data/ is public, as the GPX files' clipping is for.
func exportDemoCaptures(ctx context.Context, pool *pgxpool.Pool, owner string, span demoSpan) ([]demoManifestCapture, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.osm_type, s.osm_id, COALESCE(s.name, ''), c.captured_at
		FROM spot_captures c JOIN spots s ON s.id = c.spot_id
		WHERE c.user_id = $1 AND c.captured_at BETWEEN $2 AND $3
		  AND NOT EXISTS (SELECT 1 FROM privacy_zones z
		                  WHERE z.user_id = $1 AND ST_DWithin(z.center, s.geom::geography, z.radius_m))
		ORDER BY c.captured_at`,
		owner, time.UnixMilli(span.start), time.UnixMilli(span.end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var captures []demoManifestCapture
	for rows.Next() {
		var c demoManifestCapture
		if err := rows.Scan(&c.OSMType, &c.OSMID, &c.Name, &c.CapturedAt); err != nil {
			return nil, err
		}
		c.CapturedAt = c.CapturedAt.UTC()
		captures = append(captures, c)
	}
	return captures, rows.Err()
}

// mergeDemoCaptures adds captures to the manifest's, one per place: an export of a place
// already there replaces it.
func mergeDemoCaptures(existing, captures []demoManifestCapture) []demoManifestCapture {
	for _, c := range captures {
		i := slices.IndexFunc(existing, func(e demoManifestCapture) bool { return e.OSMType == c.OSMType && e.OSMID == c.OSMID })
		if i >= 0 {
			existing[i] = c
		} else {
			existing = append(existing, c)
		}
	}
	return existing
}

// exportDemoActivity writes one activity's GPX file and photos, and returns its manifest
// entry, its owner and the span of its exported points.
func exportDemoActivity(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, outDir, activityID string) (string, demoManifestEntry, string, demoSpan, error) {
	var (
		owner        string
		activityType string
		name         *string
		description  *string
	)
	err := pool.QueryRow(ctx,
		`SELECT user_id, activity_type, name, description FROM activities WHERE id = $1`, activityID,
	).Scan(&owner, &activityType, &name, &description)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", demoManifestEntry{}, "", demoSpan{}, errors.New("not found")
	}
	if err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	points, err := ingest.DisplayedPoints(ctx, pool, store, activityID)
	if err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	if points == nil {
		return "", demoManifestEntry{}, "", demoSpan{}, errors.New("no visible track: hidden entirely by Private locations")
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
	filename, err := freeDemoFilename(outDir, points[0].Time.UTC().Format("2006-01-02")+" "+export.Slug(label), ".gpx")
	if err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}

	f, err := os.OpenFile(filepath.Join(outDir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	if err := export.WriteGPX(f, activityType, points); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	if err := f.Close(); err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	photos, err := exportDemoPhotos(ctx, pool, store, outDir, activityID, strings.TrimSuffix(filename, ".gpx"), points)
	if err != nil {
		return "", demoManifestEntry{}, "", demoSpan{}, err
	}
	entry.Photos = photos
	return filename, entry, owner, demoSpan{points[0].Time.UnixMilli(), points[len(points)-1].Time.UnixMilli()}, nil
}

// exportDemoPhotos copies an activity's photos out: each one's stored image and thumbnail —
// already resized, with no EXIF, as the upload left them (§4.27) — into outDir/photos/ as
// "<base> <n>.<ext>" and "<base> <n>-thumb.<ext>", base being the GPX file's name, and its
// fields for the manifest. route_at is clamped to the exported track's span, as every read
// clamps it to the drawn track (photoSelect), so a moment Private locations or Edit track cut
// away isn't carried into the file.
func exportDemoPhotos(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, outDir, activityID, base string, points []parse.Point) ([]demoManifestPhoto, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, image_key, taken_at, route_at, COALESCE(caption, ''), content_type, thumb_content_type, width, height
		FROM activity_photos WHERE activity_id = $1 ORDER BY route_at, created_at`, activityID)
	if err != nil {
		return nil, err
	}
	type stored struct {
		id, imageKey, contentType, thumbContentType string
		photo                                       demoManifestPhoto
	}
	var all []stored
	for rows.Next() {
		var s stored
		if err := rows.Scan(&s.id, &s.imageKey, &s.photo.TakenAt, &s.photo.RouteAt, &s.photo.Caption, &s.contentType, &s.thumbContentType, &s.photo.Width, &s.photo.Height); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	dir := filepath.Join(outDir, demoPhotoDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	start, end := points[0].Time, points[len(points)-1].Time
	photos := make([]demoManifestPhoto, 0, len(all))
	for n, s := range all {
		ext, thumbExt := allowedPhotoContentType[s.contentType], allowedPhotoContentType[s.thumbContentType]
		if ext == "" || thumbExt == "" {
			return nil, fmt.Errorf("photo %s: unexpected content type %s / %s", s.id, s.contentType, s.thumbContentType)
		}
		if ext == "jpeg" {
			ext = "jpg"
		}
		if thumbExt == "jpeg" {
			thumbExt = "jpg"
		}
		p := s.photo
		p.File = fmt.Sprintf("%s %d.%s", base, n+1, ext)
		p.Thumb = fmt.Sprintf("%s %d-thumb.%s", base, n+1, thumbExt)
		for _, img := range []struct{ key, file string }{
			{s.imageKey, p.File},
			{photoThumbKey(s.imageKey), p.Thumb},
		} {
			if err := copyDemoObject(ctx, store, img.key, filepath.Join(dir, img.file)); err != nil {
				return nil, fmt.Errorf("photo %s: %w", s.id, err)
			}
		}
		if p.RouteAt.Before(start) {
			p.RouteAt = start
		} else if p.RouteAt.After(end) {
			p.RouteAt = end
		}
		p.RouteAt = p.RouteAt.UTC()
		if p.TakenAt != nil {
			t := p.TakenAt.UTC()
			p.TakenAt = &t
		}
		photos = append(photos, p)
	}
	return photos, nil
}

// copyDemoObject writes one stored object to a new file, never over an existing one.
func copyDemoObject(ctx context.Context, store *storage.Store, key, file string) error {
	r, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(file)
		return err
	}
	return f.Close()
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
