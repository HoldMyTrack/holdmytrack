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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
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
// (parse.ByExtension: .gpx, .tcx, .fit, .json), plus manifest.json. They are real activities
// on a live deployment, written out as GPX by `export-demo-activities` (demo_export.go)
// exactly as their owner sees them, and reviewed by hand before being committed.
//
//go:embed demo_data
var demoData embed.FS

// demoManifestFile is demo_data/'s one non-activity file: what the raw files can't carry
// themselves — per-file overrides (not every file needs one), the account's Stories and its
// Spots captures.
const demoManifestFile = "manifest.json"

// demoManifest is manifest.json: Activities keyed by filename, Stories naming their
// activities by filename too, and SpotCaptures.
type demoManifest struct {
	Activities   map[string]demoManifestEntry `json:"activities"`
	Stories      []demoManifestStory          `json:"stories,omitempty"`
	SpotCaptures []demoManifestCapture        `json:"spot_captures,omitempty"`
}

// demoManifestCapture is one place the account has captured (§4.25). A capture belongs to the
// account, not to an activity. The place is named by its OSM key, which, unlike spots.id, is
// the same on every deployment that loaded it. Name is only for someone reading the manifest;
// the seed ignores it.
type demoManifestCapture struct {
	OSMType    string    `json:"osm_type"`
	OSMID      int64     `json:"osm_id"`
	Name       string    `json:"name,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
}

// demoManifestStory is one Story the seed gives the account (§4.23): a name, a description,
// and its activities by filename. Stories are matched by name on a re-run.
type demoManifestStory struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Activities  []string `json:"activities"`
}

// demoManifestEntry is one file's overrides. Name is the activity's display name — no file
// parser sets parse.Activity.Name, so without it the seeded row gets the generic
// "<Type> - <date>" every unnamed activity gets. Type is the activity_type, needed for files
// that don't carry their own: a synced activity (Health Connect, in-app recording) reports
// its type in the sync request, not inside the JSON payload the export copies out.
// Description is the activity's free-text description, which no file carries either.
// Photos are the activity's photos (§4.27), their images in demo_data/photos/.
type demoManifestEntry struct {
	Name        string              `json:"name,omitempty"`
	Type        string              `json:"type,omitempty"`
	Description string              `json:"description,omitempty"`
	Photos      []demoManifestPhoto `json:"photos,omitempty"`
}

// demoPhotoDir is the demo_data/ subdirectory the photos' images are in — a directory, so
// demoFiles never mistakes one for an activity file.
const demoPhotoDir = "photos"

// demoManifestPhoto is one photo on a demo activity: its resized image and thumbnail as stored
// (files in demoPhotoDir, JPEG or WebP by extension), and the activity_photos fields that
// aren't the image itself. RouteAt is a moment on the activity's track, which the exported GPX
// keeps the timestamps of, so it lands on the same spot after a re-ingest.
type demoManifestPhoto struct {
	File    string     `json:"file"`
	Thumb   string     `json:"thumb"`
	TakenAt *time.Time `json:"taken_at,omitempty"`
	RouteAt time.Time  `json:"route_at"`
	Caption string     `json:"caption,omitempty"`
	Width   int        `json:"width"`
	Height  int        `json:"height"`
}

// demoPhotoContentType is a demo photo file's content type, from its extension: the two the
// photo endpoints store (allowedPhotoContentType), or "" for anything else.
func demoPhotoContentType(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// loadDemoManifest reads demo_data/manifest.json. A missing manifest is an empty one; a
// manifest naming a file that isn't there fails — an activity entry or a Story's activity
// alike — since that's a typo or a file deleted without its entry, and would otherwise pass
// silently. So does a Story the API itself would refuse: no name, a name or description over
// the limit, two Stories with one name (a re-run couldn't tell them apart), or no activities;
// and a capture with no place or time, or two of one place (the table keeps one per place).
func loadDemoManifest(fsys fs.FS, files []string) (demoManifest, error) {
	manifest := demoManifest{Activities: map[string]demoManifestEntry{}}
	b, err := fs.ReadFile(fsys, demoManifestFile)
	if errors.Is(err, fs.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return demoManifest{}, err
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return demoManifest{}, fmt.Errorf("%s: %w", demoManifestFile, err)
	}
	if manifest.Activities == nil {
		manifest.Activities = map[string]demoManifestEntry{}
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f] = true
	}
	photoFiles := map[string]bool{}
	for f, entry := range manifest.Activities {
		if !present[f] {
			return demoManifest{}, fmt.Errorf("%s: entry %q has no matching file", demoManifestFile, f)
		}
		for _, p := range entry.Photos {
			for _, img := range []string{p.File, p.Thumb} {
				if demoPhotoContentType(img) == "" {
					return demoManifest{}, fmt.Errorf("%s: %q: photo %q must be .jpg or .webp", demoManifestFile, f, img)
				}
				if photoFiles[img] {
					return demoManifest{}, fmt.Errorf("%s: photo file %q is listed twice", demoManifestFile, img)
				}
				photoFiles[img] = true
				if _, err := fs.Stat(fsys, path.Join(demoPhotoDir, img)); err != nil {
					return demoManifest{}, fmt.Errorf("%s: %q: photo %q has no matching file", demoManifestFile, f, img)
				}
			}
			if p.RouteAt.IsZero() || p.Width <= 0 || p.Height <= 0 {
				return demoManifest{}, fmt.Errorf("%s: %q: photo %q needs route_at, width and height", demoManifestFile, f, p.File)
			}
			if len([]rune(p.Caption)) > maxPhotoCaptionLen {
				return demoManifest{}, fmt.Errorf("%s: %q: photo %q: caption over %d characters", demoManifestFile, f, p.File, maxPhotoCaptionLen)
			}
		}
	}
	names := map[string]bool{}
	for _, st := range manifest.Stories {
		switch {
		case strings.TrimSpace(st.Name) == "" || len([]rune(st.Name)) > maxStoryNameLen:
			return demoManifest{}, fmt.Errorf("%s: story name %q must be 1–%d characters", demoManifestFile, st.Name, maxStoryNameLen)
		case len([]rune(st.Description)) > maxStoryDescriptionLen:
			return demoManifest{}, fmt.Errorf("%s: story %q: description over %d characters", demoManifestFile, st.Name, maxStoryDescriptionLen)
		case names[st.Name]:
			return demoManifest{}, fmt.Errorf("%s: two stories named %q", demoManifestFile, st.Name)
		case len(st.Activities) == 0:
			return demoManifest{}, fmt.Errorf("%s: story %q has no activities", demoManifestFile, st.Name)
		}
		names[st.Name] = true
		for _, f := range st.Activities {
			if !present[f] {
				return demoManifest{}, fmt.Errorf("%s: story %q: activity %q has no matching file", demoManifestFile, st.Name, f)
			}
		}
	}
	captured := map[demoManifestCapture]bool{}
	for _, c := range manifest.SpotCaptures {
		key := demoManifestCapture{OSMType: c.OSMType, OSMID: c.OSMID}
		switch {
		case c.OSMType != "node" && c.OSMType != "way" && c.OSMType != "relation":
			return demoManifest{}, fmt.Errorf("%s: capture of %s %d: osm_type must be node, way or relation", demoManifestFile, c.OSMType, c.OSMID)
		case c.OSMID <= 0 || c.CapturedAt.IsZero():
			return demoManifest{}, fmt.Errorf("%s: capture of %s %d needs osm_id and captured_at", demoManifestFile, c.OSMType, c.OSMID)
		case captured[key]:
			return demoManifest{}, fmt.Errorf("%s: %s %d is captured twice", demoManifestFile, c.OSMType, c.OSMID)
		}
		captured[key] = true
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
	// Each file's activity, for the Stories below.
	activityIDs := make(map[string]string, len(names))
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

		entry := manifest.Activities[filename]
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
		activityIDs[filename] = result.ActivityID

		// Applied after every ingest.Process call, whether it persisted a new row or found
		// one already there (ON CONFLICT DO NOTHING still returns the existing row's id) — so
		// re-running the seed always leaves the manifest's names in place, not only the one
		// time a row is first inserted.
		if entry.Name != "" || entry.Description != "" {
			if _, err := pool.Exec(ctx,
				`UPDATE activities SET name = COALESCE(NULLIF($1, ''), name), description = COALESCE(NULLIF($2, ''), description) WHERE id = $3`,
				entry.Name, entry.Description, result.ActivityID,
			); err != nil {
				log.Error("demo customer seed: name/description override failed", "err", err, "file", filename)
				failed++
			}
		}
		if err := seedDemoPhotos(ctx, pool, store, fsys, DemoCustomerUserID, result.ActivityID, entry.Photos); err != nil {
			log.Error("demo customer seed: photos failed", "err", err, "file", filename)
			failed++
		}
	}

	if failed == 0 {
		if err := seedDemoStories(ctx, pool, DemoCustomerUserID, manifest.Stories, activityIDs); err != nil {
			return fmt.Errorf("seed demo customer: %w", err)
		}
	}
	missing, err := seedDemoCaptures(ctx, pool, DemoCustomerUserID, manifest.SpotCaptures)
	if err != nil {
		return fmt.Errorf("seed demo customer: %w", err)
	}
	for _, c := range missing {
		log.Warn("demo customer seed: captured place not loaded here, skipped", "osm_type", c.OSMType, "osm_id", c.OSMID, "name", c.Name)
	}

	log.Info("demo customer seed: done", "ingested", ingested, "already_present", skipped, "failed", failed, "total", len(names), "stories", len(manifest.Stories),
		"captures", len(manifest.SpotCaptures)-len(missing))
	if failed > 0 {
		return fmt.Errorf("seed demo customer: %d of %d files failed", failed, len(names))
	}
	return nil
}

// seedDemoPhotos gives a seeded activity of userID's (the Demo Customer, but a parameter so
// tests can use their own) the manifest's photos: both images into storage where the photo
// endpoints read them (photoKey, photoThumbKey), and the row. Each photo's id is derived from
// the account and its file name, so a re-run updates the same row and objects rather than
// adding a copy, as it does an activity's name.
func seedDemoPhotos(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, fsys fs.FS, userID, activityID string, photos []demoManifestPhoto) error {
	for _, p := range photos {
		var photoID string
		if err := pool.QueryRow(ctx, `SELECT md5('demo-photo/' || $1 || '/' || $2)::uuid::text`, userID, p.File).Scan(&photoID); err != nil {
			return err
		}
		var size int
		for _, img := range []struct{ file, key string }{
			{p.File, photoKey(userID, photoID)},
			{p.Thumb, photoThumbKey(photoKey(userID, photoID))},
		} {
			b, err := fs.ReadFile(fsys, path.Join(demoPhotoDir, img.file))
			if err != nil {
				return err
			}
			if err := store.Put(ctx, img.key, bytes.NewReader(b), int64(len(b))); err != nil {
				return fmt.Errorf("photo %q: %w", img.file, err)
			}
			size += len(b)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO activity_photos (id, user_id, activity_id, taken_at, route_at, caption, content_type, thumb_content_type, width, height, bytes, image_key)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET activity_id = EXCLUDED.activity_id, taken_at = EXCLUDED.taken_at,
				route_at = EXCLUDED.route_at, caption = EXCLUDED.caption, content_type = EXCLUDED.content_type,
				thumb_content_type = EXCLUDED.thumb_content_type, width = EXCLUDED.width, height = EXCLUDED.height,
				bytes = EXCLUDED.bytes`,
			photoID, userID, activityID, p.TakenAt, p.RouteAt, p.Caption,
			demoPhotoContentType(p.File), demoPhotoContentType(p.Thumb), p.Width, p.Height, size, photoKey(userID, photoID),
		); err != nil {
			return fmt.Errorf("photo %q: %w", p.File, err)
		}
	}
	return nil
}

// seedDemoCaptures gives the account (the Demo Customer, but a parameter so tests can use their
// own) the manifest's captures, each at the manifest's time, so a re-run fixes one that
// drifted. A capture the manifest doesn't name is left alone; --reset is what removes those.
// A place this deployment hasn't loaded (import-spots, §4.25) can't be captured: it's returned
// rather than failing the seed, since a development stack often has no places at all.
func seedDemoCaptures(ctx context.Context, pool *pgxpool.Pool, userID string, captures []demoManifestCapture) ([]demoManifestCapture, error) {
	var missing []demoManifestCapture
	for _, c := range captures {
		tag, err := pool.Exec(ctx, `
			INSERT INTO spot_captures (user_id, spot_id, captured_at)
			SELECT $1, id, $4 FROM spots WHERE osm_type = $2 AND osm_id = $3
			ON CONFLICT (user_id, spot_id) DO UPDATE SET captured_at = EXCLUDED.captured_at`,
			userID, c.OSMType, c.OSMID, c.CapturedAt)
		if err != nil {
			return nil, fmt.Errorf("capture of %s %d: %w", c.OSMType, c.OSMID, err)
		}
		if tag.RowsAffected() == 0 {
			missing = append(missing, c)
		}
	}
	return missing, nil
}

// seedDemoStories gives the account (the Demo Customer, but a parameter so tests can use their
// own) the manifest's Stories, each matched by name: created if
// it isn't there, and otherwise given the manifest's description and exactly its activities —
// so a plain re-run fixes a Story that drifted, as it does an activity's name. A Story the
// manifest doesn't name is left alone; --reset is what removes those. One transaction, and one
// tile-version bump if any Story's activities changed (§4.2.6): the tracks tiles take a
// `story` filter.
//
// Each Story is dated the day after its last activity ended, as someone would make one after a
// trip, rather than the moment of the seed: the Stories are listed newest first, so they then
// read in the order they were visited, not the manifest's.
func seedDemoStories(ctx context.Context, pool *pgxpool.Pool, userID string, stories []demoManifestStory, activityIDs map[string]string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("stories: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	changed := false
	for _, st := range stories {
		ids := make([]string, 0, len(st.Activities))
		for _, f := range st.Activities {
			ids = append(ids, activityIDs[f])
		}
		var storyID string
		err := tx.QueryRow(ctx, `SELECT id FROM stories WHERE user_id = $1 AND name = $2 ORDER BY created_at LIMIT 1`,
			userID, st.Name).Scan(&storyID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = tx.QueryRow(ctx, `INSERT INTO stories (user_id, name, description) VALUES ($1, $2, NULLIF($3, '')) RETURNING id`,
				userID, st.Name, st.Description).Scan(&storyID)
		case err == nil:
			_, err = tx.Exec(ctx, `UPDATE stories SET description = NULLIF($2, ''), updated_at = NOW() WHERE id = $1 AND description IS DISTINCT FROM NULLIF($2, '')`,
				storyID, st.Description)
		}
		if err != nil {
			return fmt.Errorf("story %q: %w", st.Name, err)
		}
		removed, err := tx.Exec(ctx, `DELETE FROM story_activities WHERE story_id = $1 AND NOT activity_id = ANY($2::uuid[])`, storyID, ids)
		if err != nil {
			return fmt.Errorf("story %q: %w", st.Name, err)
		}
		added, err := tx.Exec(ctx, `
			INSERT INTO story_activities (story_id, activity_id) SELECT $1, unnest($2::uuid[])
			ON CONFLICT DO NOTHING`, storyID, ids)
		if err != nil {
			return fmt.Errorf("story %q: %w", st.Name, err)
		}
		if removed.RowsAffected() > 0 || added.RowsAffected() > 0 {
			changed = true
		}
		if _, err := tx.Exec(ctx, `
			UPDATE stories s SET created_at = d.at, updated_at = d.at
			FROM (SELECT MAX(started_at + make_interval(secs => COALESCE(duration_seconds, 0))) + INTERVAL '1 day' AS at
			      FROM activities WHERE id = ANY($2::uuid[])) d
			WHERE s.id = $1 AND (s.created_at IS DISTINCT FROM d.at OR s.updated_at IS DISTINCT FROM d.at)`,
			storyID, ids); err != nil {
			return fmt.Errorf("story %q: %w", st.Name, err)
		}
	}
	if changed {
		if err := fog.BumpMapVersion(ctx, tx, userID); err != nil {
			return fmt.Errorf("stories: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// resetDemoCustomer deletes every activity the demo account has, with everything derived from
// them, so the seed that follows starts from nothing. The DB side cascades from activities
// (streams, tile masks, country/region matches, photos); fog_tiles is per user, not per activity, so
// it's deleted explicitly — ingest recreates each row a new activity touches, and a row left
// behind would keep a tile only the old history reached. Object storage has no foreign keys,
// so its prefixes are swept first, in the same log-and-continue style demo_purge.go uses.
// heatmap_cap goes back to its column default: the old history's value would scale the new
// one's heatmap until the worker's daily cap sweep caught up.
//
// The worker is quiesced for this account first: a previous seed can leave a render_fog job
// queued behind its ingests, and one already running would rewrite fog_tiles rows (and their
// objects) right after they were deleted. The queued ones are dropped — the seed that follows
// enqueues its own — and a running one is waited out.
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

	prefixes := []string{"raw/" + DemoCustomerUserID + "/", "fog/" + DemoCustomerUserID + "/", "heatmap/" + DemoCustomerUserID + "/", "photos/" + DemoCustomerUserID + "/"}
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
		// Its Stories too: the seed recreates the manifest's, and nothing else should survive.
		`DELETE FROM stories WHERE user_id = $1`,
		// And its captures, which belong to the account rather than to an activity.
		`DELETE FROM spot_captures WHERE user_id = $1`,
		`DELETE FROM fog_tiles WHERE user_id = $1`,
		// Its tiles are about to be rendered from scratch; nothing cached before this is its.
		`UPDATE users SET heatmap_cap = DEFAULT, map_version = map_version + 1 WHERE id = $1`,
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
