// Package export builds a copy of everything an account holds, for its owner to download
// (SPEC FR-1.12, IMPLEMENTATION.md §4.29): zip parts in object storage that the worker's
// `export` job writes and httpapi serves.
package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// PartLimit is roughly how big one zip part gets: a part is closed once it passes this, so
// one ends at most a file (an upload is at most 512 MB) past it. Google Takeout's default.
// A variable so a test can split a small archive.
var PartLimit int64 = 2 << 30

// Lifetime is how long a finished export can be downloaded before the worker removes it.
const Lifetime = 7 * 24 * time.Hour

// Prefix is where one export's parts are stored, under the owner's id so that deleting the
// account (internal/worker/account_purge.go) takes them too.
func Prefix(userID, exportID string) string { return "exports/" + userID + "/" + exportID + "/" }

// PartKey is part n's object key, numbered from 1.
func PartKey(userID, exportID string, n int) string {
	return fmt.Sprintf("%s%d.zip", Prefix(userID, exportID), n)
}

// FileName is what a part is called when downloaded, "holdmytrack-2026-10-03-2-of-3.zip".
func FileName(requested time.Time, n, of int) string {
	return fmt.Sprintf("holdmytrack-%s-%d-of-%d.zip", requested.UTC().Format("2006-01-02"), n, of)
}

// Build writes the account's archive and returns each part's size. lang is the README's
// language. Parts a previous, failed attempt left behind are removed first, so a retried job
// starts clean.
//
// The archive holds, spread over the parts as they fill (extracted into one folder they make
// one tree):
//   - README.txt: what is where.
//   - originals/: every file uploaded or synced, as it arrived, once each — the activity's own
//     data, before Private locations clipped it.
//   - tracks/: every activity as GPX, as the owner's map shows it — clipped, with its track
//     edit applied (ingest.DisplayedPoints).
//   - photos/: the stored copy of each photo.
//   - account.json: the account, every activity's details and which files are its, Stories,
//     Private locations and captured places. Written last, so it can name every file.
//
// Each part is built in a temporary file and uploaded with its size known: an upload of
// unknown length would make the store's client buffer half a gigabyte at a time.
func Build(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, exportID, lang string) ([]int64, error) {
	if err := store.RemoveByPrefix(ctx, Prefix(userID, exportID)); err != nil {
		return nil, err
	}
	if err := waitForEdits(ctx, pool, userID); err != nil {
		return nil, err
	}
	a := &archive{ctx: ctx, store: store, userID: userID, exportID: exportID}
	defer a.abandon()

	doc, err := loadAccount(ctx, pool, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc.ExportedAt = now
	l := i18n.Get(lang)
	if err := a.add("README.txt", false, func(w io.Writer) error {
		_, err := io.WriteString(w, strings.ReplaceAll(l.T("export.readme", "date", now.Format("2006-01-02"))+"\n", "\n", "\r\n"))
		return err
	}); err != nil {
		return nil, err
	}

	// Each activity is named by its local start where it was recorded (IMPLEMENTATION.md §4.30).
	accountLoc, err := time.LoadLocation(doc.Account.Timezone)
	if err != nil {
		accountLoc = time.UTC
	}
	locs := map[string]*time.Location{}
	locationOf := func(tz string) *time.Location {
		loc, ok := locs[tz]
		if !ok {
			if loc, err = time.LoadLocation(tz); err != nil {
				loc = accountLoc
			}
			locs[tz] = loc
		}
		return loc
	}
	names := map[string]bool{}
	originals := map[string]string{} // raw key -> its file in the archive
	for i := range doc.Activities {
		act := &doc.Activities[i]
		label := act.Type
		if act.Name != "" {
			label = act.Name
		}
		base := uniqueName(names, act.StartedAt.In(locationOf(act.Timezone)).Format("2006-01-02 1504")+" "+Slug(label))

		if act.rawKey != "" {
			if file, ok := originals[act.rawKey]; ok {
				act.Original = file
			} else {
				file := "originals/" + base + strings.ToLower(path.Ext(act.rawKey))
				if err := a.add(file, false, func(w io.Writer) error { return a.copyObject(w, act.rawKey) }); err != nil {
					return nil, fmt.Errorf("activity %s: original: %w", act.ID, err)
				}
				originals[act.rawKey] = file
				act.Original = file
			}
		}

		// A track that can't be rebuilt (a raw payload that no longer parses) is marked
		// rather than failing the whole archive; the original is still in it.
		points, err := ingest.DisplayedPoints(ctx, pool, store, act.ID)
		switch {
		case err != nil:
			act.TrackUnavailable = true
		case points != nil:
			file := "tracks/" + base + ".gpx"
			if err := a.add(file, true, func(w io.Writer) error { return WriteGPX(w, act.Type, points) }); err != nil {
				return nil, fmt.Errorf("activity %s: track: %w", act.ID, err)
			}
			act.Track = file
		}

		for n := range act.Photos {
			p := &act.Photos[n]
			file := fmt.Sprintf("photos/%s %d.%s", base, n+1, p.ext)
			if err := a.add(file, false, func(w io.Writer) error { return a.copyObject(w, p.key) }); err != nil {
				return nil, fmt.Errorf("activity %s: photo: %w", act.ID, err)
			}
			p.File = file
		}
	}

	if err := a.add("account.json", true, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}); err != nil {
		return nil, err
	}
	if err := a.finishPart(); err != nil {
		return nil, err
	}
	return a.sizes, nil
}

// editWait bounds how long Build waits for the account's track edits and Private location
// changes to finish: until they do, an activity's track can't be rebuilt (DisplayedPoints).
// They take seconds; a variable so a test needn't wait.
var editWait = 2 * time.Minute

func waitForEdits(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	deadline := time.Now().Add(editWait)
	for {
		var pending bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM activities WHERE user_id = $1 AND edit_pending)`, userID).Scan(&pending); err != nil {
			return fmt.Errorf("pending edits: %w", err)
		}
		if !pending {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("track edits still being applied")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// uniqueName is base, or base 2, base 3… — the first not yet taken.
func uniqueName(taken map[string]bool, base string) string {
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	taken[name] = true
	return name
}

// archive is the zip being written: the current part's temporary file, and the sizes of the
// parts already uploaded.
type archive struct {
	ctx              context.Context
	store            *storage.Store
	userID, exportID string
	file             *os.File
	counter          *countingWriter
	zw               *zip.Writer
	sizes            []int64
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// add writes one file into the archive, starting a new part first when the current one has
// passed PartLimit. compress is false for what is already compressed (photos, .fit and .zip
// uploads are not worth the CPU) — originals are stored as they are.
func (a *archive) add(name string, compress bool, write func(io.Writer) error) error {
	if a.zw != nil {
		// The zip writer buffers; what has reached the file is what counts.
		if err := a.zw.Flush(); err != nil {
			return err
		}
		if a.counter.n >= PartLimit {
			if err := a.finishPart(); err != nil {
				return err
			}
		}
	}
	if a.zw == nil {
		f, err := os.CreateTemp("", "holdmytrack-export-*.zip")
		if err != nil {
			return err
		}
		a.file = f
		a.counter = &countingWriter{w: f}
		a.zw = zip.NewWriter(a.counter)
	}
	method := zip.Store
	if compress {
		method = zip.Deflate
	}
	w, err := a.zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		return err
	}
	return write(w)
}

// finishPart closes the current part and uploads it as the next part number.
func (a *archive) finishPart() error {
	if a.zw == nil {
		return nil
	}
	if err := a.zw.Close(); err != nil {
		return err
	}
	size := a.counter.n
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := a.store.Put(a.ctx, PartKey(a.userID, a.exportID, len(a.sizes)+1), a.file, size); err != nil {
		return err
	}
	a.sizes = append(a.sizes, size)
	a.abandon()
	return nil
}

// abandon drops the current part's temporary file, uploaded or not.
func (a *archive) abandon() {
	if a.file != nil {
		a.file.Close()
		os.Remove(a.file.Name())
	}
	a.file, a.counter, a.zw = nil, nil, nil
}

func (a *archive) copyObject(w io.Writer, key string) error {
	r, err := a.store.Get(a.ctx, key)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(w, r)
	return err
}

// The account.json document.
type document struct {
	ExportedAt       time.Time         `json:"exported_at"`
	Account          accountInfo       `json:"account"`
	Activities       []activity        `json:"activities"`
	Stories          []story           `json:"stories"`
	PrivateLocations []privateLocation `json:"private_locations"`
	CapturedPlaces   []capturedPlace   `json:"captured_places"`
}

type accountInfo struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name,omitempty"`
	Country     string    `json:"country,omitempty"`
	Timezone    string    `json:"timezone"`
	Language    string    `json:"language,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type activity struct {
	ID               string    `json:"id"`
	Name             string    `json:"name,omitempty"`
	Type             string    `json:"type"`
	Source           string    `json:"source"`
	Description      string    `json:"description,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	Timezone         string    `json:"timezone"` // where it was recorded, an IANA name
	DistanceMeters   *float64  `json:"distance_m,omitempty"`
	ElapsedSeconds   *int64    `json:"elapsed_s,omitempty"`
	MovingSeconds    *int64    `json:"moving_s,omitempty"`
	ElevationGainM   *float64  `json:"elevation_gain_m,omitempty"`
	Original         string    `json:"original,omitempty"`
	Track            string    `json:"track,omitempty"`
	TrackUnavailable bool      `json:"track_unavailable,omitempty"`
	Photos           []photo   `json:"photos,omitempty"`
	rawKey           string
}

type photo struct {
	File    string     `json:"file"`
	Caption string     `json:"caption,omitempty"`
	TakenAt *time.Time `json:"taken_at,omitempty"`
	RouteAt time.Time  `json:"route_at"`
	key     string
	ext     string
}

type story struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Activities  []string  `json:"activities"`
}

type privateLocation struct {
	Name    string  `json:"name,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM int     `json:"radius_m"`
}

type capturedPlace struct {
	Name       string    `json:"name,omitempty"`
	Category   string    `json:"category"`
	OSM        string    `json:"openstreetmap"`
	CapturedAt time.Time `json:"captured_at"`
}

// photoExt is a stored photo's file extension, by the content type httpapi checked it as.
var photoExt = map[string]string{"image/jpeg": "jpg", "image/webp": "webp"}

func loadAccount(ctx context.Context, pool *pgxpool.Pool, userID string) (*document, error) {
	doc := &document{Activities: []activity{}, Stories: []story{}, PrivateLocations: []privateLocation{}, CapturedPlaces: []capturedPlace{}}
	a := &doc.Account
	if err := pool.QueryRow(ctx, `
		SELECT email, COALESCE(display_name, ''), COALESCE(country, ''), timezone, COALESCE(locale, ''), created_at
		FROM users WHERE id = $1 AND deleted_at IS NULL`, userID,
	).Scan(&a.Email, &a.DisplayName, &a.Country, &a.Timezone, &a.Language, &a.CreatedAt); err != nil {
		return nil, fmt.Errorf("account: %w", err)
	}
	a.CreatedAt = a.CreatedAt.UTC()

	rows, err := pool.Query(ctx, `
		SELECT id, COALESCE(name, ''), activity_type, source, COALESCE(description, ''), started_at, timezone,
		       distance_meters::float8, duration_seconds, moving_seconds, elevation_gain_m::float8,
		       COALESCE(raw_payload_key, '')
		FROM activities WHERE user_id = $1 ORDER BY started_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("activities: %w", err)
	}
	index := map[string]int{}
	for rows.Next() {
		var v activity
		if err := rows.Scan(&v.ID, &v.Name, &v.Type, &v.Source, &v.Description, &v.StartedAt, &v.Timezone,
			&v.DistanceMeters, &v.ElapsedSeconds, &v.MovingSeconds, &v.ElevationGainM, &v.rawKey); err != nil {
			rows.Close()
			return nil, fmt.Errorf("activities: %w", err)
		}
		v.StartedAt = v.StartedAt.UTC()
		index[v.ID] = len(doc.Activities)
		doc.Activities = append(doc.Activities, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("activities: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT id, activity_id, COALESCE(caption, ''), taken_at, route_at, content_type, image_key
		FROM activity_photos WHERE user_id = $1 ORDER BY activity_id, route_at, created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("photos: %w", err)
	}
	for rows.Next() {
		var id, activityID, contentType, imageKey string
		var p photo
		if err := rows.Scan(&id, &activityID, &p.Caption, &p.TakenAt, &p.RouteAt, &contentType, &imageKey); err != nil {
			rows.Close()
			return nil, fmt.Errorf("photos: %w", err)
		}
		i, ok := index[activityID]
		if !ok {
			continue
		}
		p.key, p.ext = imageKey, photoExt[contentType]
		if p.ext == "" {
			p.ext = "jpg"
		}
		p.RouteAt = p.RouteAt.UTC()
		if p.TakenAt != nil {
			t := p.TakenAt.UTC()
			p.TakenAt = &t
		}
		doc.Activities[i].Photos = append(doc.Activities[i].Photos, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT s.name, COALESCE(s.description, ''), s.created_at,
		       COALESCE(array_agg(sa.activity_id::text ORDER BY sa.activity_id) FILTER (WHERE sa.activity_id IS NOT NULL), '{}')
		FROM stories s LEFT JOIN story_activities sa ON sa.story_id = s.id
		WHERE s.user_id = $1 GROUP BY s.id ORDER BY s.created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("stories: %w", err)
	}
	for rows.Next() {
		var s story
		if err := rows.Scan(&s.Name, &s.Description, &s.CreatedAt, &s.Activities); err != nil {
			rows.Close()
			return nil, fmt.Errorf("stories: %w", err)
		}
		s.CreatedAt = s.CreatedAt.UTC()
		doc.Stories = append(doc.Stories, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stories: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT COALESCE(name, ''), ST_Y(center::geometry), ST_X(center::geometry), radius_m
		FROM privacy_zones WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("private locations: %w", err)
	}
	for rows.Next() {
		var z privateLocation
		if err := rows.Scan(&z.Name, &z.Lat, &z.Lon, &z.RadiusM); err != nil {
			rows.Close()
			return nil, fmt.Errorf("private locations: %w", err)
		}
		doc.PrivateLocations = append(doc.PrivateLocations, z)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("private locations: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT COALESCE(s.name, ''), s.category, s.osm_type || '/' || s.osm_id, c.captured_at
		FROM spot_captures c JOIN spots s ON s.id = c.spot_id
		WHERE c.user_id = $1 ORDER BY c.captured_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("captures: %w", err)
	}
	for rows.Next() {
		var c capturedPlace
		if err := rows.Scan(&c.Name, &c.Category, &c.OSM, &c.CapturedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("captures: %w", err)
		}
		c.OSM = "https://www.openstreetmap.org/" + c.OSM
		c.CapturedAt = c.CapturedAt.UTC()
		doc.CapturedPlaces = append(doc.CapturedPlaces, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("captures: %w", err)
	}
	return doc, nil
}
