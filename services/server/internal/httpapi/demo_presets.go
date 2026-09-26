package httpapi

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// DemoCustomerUserID is the one persistent, shared demo account every "Try Demo" visitor's
// session is pointed at (handleDemoStart) — not a fresh row created and re-seeded per visitor.
// Must match migrations/0006_demo_customer.sql's fixed id. Its demo_expires_at is a
// far-future timestamp, not NULL, so it stays isDemo == true (read-only, verification-exempt)
// without ever being swept by internal/worker/demo_purge.go.
const DemoCustomerUserID = "22222222-2222-2222-2222-222222222222"

// demo_data/ holds the account's history as raw activity files in any format ingest parses
// (parse.ByExtension: .gpx, .tcx, .fit, .json), plus manifest.json. Most of them are the
// original uploads of real activities on a live deployment, copied out by
// `export-demo-activities` (demo_export.go) and reviewed by hand before being committed.
//
//go:embed demo_data
var demoData embed.FS

// demoManifestFile is demo_data/'s one non-activity file: per-file overrides a raw file
// can't carry itself. Not every file needs an entry.
const demoManifestFile = "manifest.json"

// demoManifestEntry is one file's overrides. Name is the activity's display name — no file
// parser sets parse.Activity.Name, so without it the seeded row gets the generic
// "<Type> - <date>" every unnamed activity gets. Type is the activity_type, needed for files
// that don't carry their own: a synced activity (Health Connect, in-app recording) reports
// its type in the sync request, not inside the JSON payload the export copies out.
type demoManifestEntry struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// loadDemoManifest reads demo_data/manifest.json, keyed by filename. A missing manifest is an
// empty one; a manifest naming a file that isn't there fails, since that's a typo or a file
// deleted without its entry, and would otherwise pass silently.
func loadDemoManifest(fsys fs.FS, files []string) (map[string]demoManifestEntry, error) {
	b, err := fs.ReadFile(fsys, demoManifestFile)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]demoManifestEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest map[string]demoManifestEntry
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", demoManifestFile, err)
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f] = true
	}
	for f := range manifest {
		if !present[f] {
			return nil, fmt.Errorf("%s: entry %q has no matching file", demoManifestFile, f)
		}
	}
	return manifest, nil
}

// demoFiles lists demo_data/'s activity files, sorted, failing on any extension ingest can't
// parse — a stray README or .DS_Store would otherwise fail one by one at ingest time, after
// the files sorted ahead of it were already seeded.
func demoFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == demoManifestFile {
			continue
		}
		switch strings.ToLower(path.Ext(e.Name())) {
		case ".gpx", ".tcx", ".fit", ".json":
			names = append(names, e.Name())
		default:
			return nil, fmt.Errorf("unsupported file %q (want .gpx, .tcx, .fit or .json)", e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// SeedDemoCustomer ingests every embedded activity file in demo_data/ into
// DemoCustomerUserID, through the same real pipeline (internal/ingest.Process — parse,
// persist, fog/heatmap mask render) a real upload uses. Meant to run once, out of band (the
// `seed-demo-customer` CLI subcommand), not per visitor — see docs/ROADMAP.md's "Email
// verification + demo without real ingest" for why re-running this per "Try Demo" click would
// be far too slow. ingest.go's `ON CONFLICT (user_id, source, external_id) DO NOTHING` makes
// re-running this safe: already-ingested files are skipped, not duplicated.
//
// That same skip means a plain re-run never picks up a changed or removed file — reset wipes
// the account's activities first (resetDemoCustomer), so the seeded history ends up matching
// demo_data/ exactly.
func SeedDemoCustomer(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, reset bool) error {
	fsys, err := fs.Sub(demoData, "demo_data")
	if err != nil {
		return fmt.Errorf("seed demo customer: %w", err)
	}
	names, err := demoFiles(fsys)
	if err != nil {
		return fmt.Errorf("seed demo customer: %w", err)
	}
	manifest, err := loadDemoManifest(fsys, names)
	if err != nil {
		return fmt.Errorf("seed demo customer: %w", err)
	}

	if reset {
		if err := resetDemoCustomer(ctx, pool, store, log); err != nil {
			return fmt.Errorf("seed demo customer: %w", err)
		}
	}

	var ingested, skipped, failed int
	for _, filename := range names {
		b, err := fs.ReadFile(fsys, filename)
		if err != nil {
			log.Error("demo customer seed: read embedded file failed", "err", err, "file", filename)
			failed++
			continue
		}

		ext := path.Ext(filename)
		externalID := "demo-customer-" + strings.TrimSuffix(filename, ext)
		rawKey := "raw/" + DemoCustomerUserID + "/demo-history/" + externalID + ext
		if err := store.Put(ctx, rawKey, bytes.NewReader(b), int64(len(b))); err != nil {
			log.Error("demo customer seed: upload failed", "err", err, "file", filename)
			failed++
			continue
		}

		entry := manifest[filename]
		job := ingest.Job{
			UserID:        DemoCustomerUserID,
			Source:        "demo-customer-history",
			SourceDetail:  filename,
			ExternalID:    externalID,
			RawPayloadKey: rawKey,
			ActivityType:  entry.Type,
		}
		result, err := ingest.Process(ctx, pool, store, job)
		if err != nil {
			log.Error("demo customer seed: ingest failed", "err", err, "file", filename)
			failed++
			continue
		}
		if result.Persisted {
			ingested++
		} else {
			skipped++
		}

		// Applied after every ingest.Process call, whether it persisted a new row or found
		// one already there (ON CONFLICT DO NOTHING still returns the existing row's id) — so
		// re-running the seed always leaves the manifest's names in place, not only the one
		// time a row is first inserted.
		if entry.Name != "" {
			if _, err := pool.Exec(ctx, `UPDATE activities SET name = $1 WHERE id = $2`, entry.Name, result.ActivityID); err != nil {
				log.Error("demo customer seed: name override failed", "err", err, "file", filename)
				failed++
			}
		}
	}

	log.Info("demo customer seed: done", "ingested", ingested, "already_present", skipped, "failed", failed, "total", len(names))
	if failed > 0 {
		return fmt.Errorf("seed demo customer: %d of %d files failed", failed, len(names))
	}
	return nil
}

// resetDemoCustomer deletes every activity the demo account has, with everything derived from
// them, so the seed that follows starts from nothing. The DB side cascades from activities
// (streams, tile masks, country/region matches); fog_tiles is per user, not per activity, so
// it's deleted explicitly — ingest recreates each row a new activity touches, and a row left
// behind would keep a tile only the old history reached. Object storage has no foreign keys,
// so its prefixes are swept first, in the same log-and-continue style demo_purge.go uses.
// heatmap_cap goes back to its column default: the old history's value would scale the new
// one's heatmap until the worker's daily cap sweep caught up.
//
// The worker is quiesced for this account first: every ingest enqueues a render_fog job, so a
// previous seed can leave hundreds queued, and one already running would rewrite fog_tiles
// rows (and their objects) right after they were deleted. The queued ones are dropped — the
// seed that follows enqueues its own — and a running one is waited out.
func resetDemoCustomer(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	if err := quiesceDemoJobs(ctx, pool, log); err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	rows, err := pool.Query(ctx, `SELECT id FROM activities WHERE user_id = $1`, DemoCustomerUserID)
	if err != nil {
		return fmt.Errorf("reset: list activities: %w", err)
	}
	var activityIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("reset: scan: %w", err)
		}
		activityIDs = append(activityIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reset: rows: %w", err)
	}

	prefixes := []string{"raw/" + DemoCustomerUserID + "/", "fog/" + DemoCustomerUserID + "/", "heatmap/" + DemoCustomerUserID + "/"}
	for _, id := range activityIDs {
		prefixes = append(prefixes, "activity-masks/"+id+"/")
	}
	for _, prefix := range prefixes {
		if err := store.RemoveByPrefix(ctx, prefix); err != nil {
			log.Error("demo customer reset: storage cleanup failed, deleting rows anyway", "prefix", prefix, "err", err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reset: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, q := range []string{
		`DELETE FROM jobs WHERE user_id = $1 AND state = 'pending'`,
		`DELETE FROM activities WHERE user_id = $1`,
		`DELETE FROM fog_tiles WHERE user_id = $1`,
		`UPDATE users SET heatmap_cap = DEFAULT WHERE id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, DemoCustomerUserID); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reset: commit: %w", err)
	}
	log.Info("demo customer reset: done", "activities_removed", len(activityIDs))
	return nil
}

// demoJobLockStale bounds how long quiesceDemoJobs waits on a claimed job: past this, a job
// still claimed but never finished belongs to a worker that died mid-run, not one still going.
const demoJobLockStale = 10 * time.Minute

// quiesceDemoJobs deletes the demo account's unclaimed pending jobs and waits until none is
// running. A claim (internal/worker's claimAndRunOne) only sets locked_at — the job stays
// 'pending' until it finishes — so a running job is a pending one with a recent locked_at.
func quiesceDemoJobs(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM jobs WHERE user_id = $1 AND state = 'pending' AND locked_at IS NULL`, DemoCustomerUserID)
	if err != nil {
		return fmt.Errorf("drop queued jobs: %w", err)
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Info("demo customer reset: dropped queued jobs", "jobs", n)
	}
	for waited := false; ; waited = true {
		var running int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM jobs WHERE user_id = $1 AND state = 'pending' AND locked_at > NOW() - make_interval(secs => $2)`,
			DemoCustomerUserID, demoJobLockStale.Seconds(),
		).Scan(&running); err != nil {
			return fmt.Errorf("check running jobs: %w", err)
		}
		if running == 0 {
			return nil
		}
		if !waited {
			log.Info("demo customer reset: waiting for running jobs to finish", "jobs", running)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
