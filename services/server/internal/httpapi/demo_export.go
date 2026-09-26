package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// ExportDemoActivities copies the listed activities' original uploads, byte for byte, into
// outDir as demo_data/-ready files, and writes outDir/manifest.json with their names and
// types — the `export-demo-activities` CLI subcommand, run against a live deployment to pick
// real activities for the Demo Customer's history (SeedDemoCustomer). The raw file is copied
// as-is, not the owner's post-Private-location, post-edit view of it: whoever commits the
// result reviews and trims each file by hand, and the demo re-ingests exactly what it sees.
//
// Files are named "<YYYY-MM-DD> <slug><ext>" from the start date in UTC and the activity's
// name (or type), with the extension of the stored raw payload, which is what parse.ByExtension
// dispatches on at seed time. A file already in outDir is never overwritten; the name gets a
// -2, -3, ... suffix instead, so several exports into one directory accumulate. Any failure
// stops the export, naming the activity id.
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
		rawKey       *string
		activityType string
		name         *string
		startedAt    time.Time
	)
	err := pool.QueryRow(ctx,
		`SELECT raw_payload_key, activity_type, name, started_at FROM activities WHERE id = $1`, activityID,
	).Scan(&rawKey, &activityType, &name, &startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", demoManifestEntry{}, errors.New("not found")
	}
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	if rawKey == nil {
		return "", demoManifestEntry{}, errors.New("no raw payload stored")
	}

	entry := demoManifestEntry{Type: activityType}
	label := activityType
	if name != nil && *name != "" {
		entry.Name = *name
		label = *name
	}
	filename, err := freeDemoFilename(outDir, startedAt.UTC().Format("2006-01-02")+" "+demoSlug(label), path.Ext(*rawKey))
	if err != nil {
		return "", demoManifestEntry{}, err
	}

	obj, err := store.Get(ctx, *rawKey)
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	defer obj.Close()
	f, err := os.OpenFile(filepath.Join(outDir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", demoManifestEntry{}, err
	}
	if _, err := io.Copy(f, obj); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", demoManifestEntry{}, err
	}
	if err := f.Close(); err != nil {
		return "", demoManifestEntry{}, err
	}
	return filename, entry, nil
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
