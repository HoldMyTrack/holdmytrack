// Package unpack is the worker's `unpack` job (IMPLEMENTATION.md §4.0.1, ADR-0032): a `.zip`
// of activity files or a Google Takeout export, stored whole by the upload request, walked
// here and enqueued as one `ingest` job per activity, so the request answers as soon as the
// archive has arrived.
package unpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/takeout"
)

// AllowedExt is the activity file types a plain upload or a `.zip`'s entry may be.
var AllowedExt = map[string]bool{".gpx": true, ".fit": true, ".tcx": true}

// MaxEntries bounds how many files inside one archive are processed — not a claim that a real
// import can't have more, just where this server stops rather than enqueueing an unbounded
// number of jobs from one upload. §5.1's "one bad file in a bulk import cannot abort the
// batch" still holds beneath this cap; this is a different, coarser limit on the batch's size.
const MaxEntries = 5000

// MaxEntryBytes bounds any single file inside a zip the same way the upload limit bounds a
// plain upload — checked against the entry's own declared size before it's decompressed at
// all (§5.1's zip-bomb defense), and again against how much is actually read, since a
// declared size is something a malformed or hostile archive can simply lie about.
const MaxEntryBytes = 64 << 20

// TakeoutMarkerPrefix identifies a Google Takeout / Google Health export among a zip's own
// entry names, distinguishing it from a plain multi-file `.zip` before any of it is extracted.
// Confirmed against a real ~2000-file, 1.9 GB sample export: every entry sits under this
// prefix regardless of which Health/Fitbit sub-category was selected when it was made.
const TakeoutMarkerPrefix = "Takeout/Google Health/"

// Format names an archive's kind: FormatZip for a `.zip` of activity files, FormatTakeout for
// a Google Takeout export. The request tells them apart (IsTakeout); the job just follows.
const (
	FormatZip     = "zip"
	FormatTakeout = "takeout"
)

// IsTakeout reports whether an archive is a Takeout export, from its entry names alone.
func IsTakeout(zr *zip.Reader) bool {
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, TakeoutMarkerPrefix) {
			return true
		}
	}
	return false
}

// Key is where an archive waits in object storage for its job.
func Key(userID, batch string) string {
	return "imports/" + userID + "/" + batch + ".zip"
}

// Job is the `unpack` job's payload. Source, Batch and BatchTitle carry ingest.Job's names, so
// the Upload menu's query reads an archive waiting to be unpacked as the same import as the
// ingest jobs it turns into.
type Job struct {
	UserID     string `json:"user_id"`
	Source     string `json:"source"` // "upload" for a plain zip, "takeout"
	Format     string `json:"format"`
	Key        string `json:"key"`
	Batch      string `json:"batch"`
	BatchTitle string `json:"batch_title"`
	State      State  `json:"state"`
}

// State is how far a job has got, saved in its payload with each chunk of jobs it enqueues,
// so a run cut off by a deploy resumes where it stopped instead of enqueueing the same files
// twice. Once the job is done it's the archive's outcome, which the Upload menu reports.
type State struct {
	// Cursor counts the units — files, and Takeout activity types — already dealt with.
	Cursor    int  `json:"cursor"`
	Already   int  `json:"already"`
	Skipped   int  `json:"skipped"`
	Truncated bool `json:"truncated,omitempty"`
}

// Failure codes an unpack job is stored with (jobs.error_code).
const (
	FailUnreadable = "unreadable_archive"
	FailInternal   = "internal"
)

// errUnreadable is an archive that can't be opened as what the request recognised it as.
var errUnreadable = errors.New("unpack: unreadable archive")

// FailureCode is the error_code a failed unpack job is stored with.
func FailureCode(err error) string {
	if errors.Is(err, errUnreadable) {
		return FailUnreadable
	}
	return FailInternal
}

// Run unpacks one archive. The archive is removed from storage once the job is over either
// way, unless it failed for a reason of ours, which leaves it for a look.
func Run(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, jobID int64, payload []byte) error {
	var j Job
	if err := json.Unmarshal(payload, &j); err != nil {
		return fmt.Errorf("unpack: unmarshal job: %w", err)
	}

	f, size, err := download(ctx, store, j.Key)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // a temp file
	defer f.Close()

	err = walk(ctx, pool, jobID, &j, f, size)
	if err == nil || errors.Is(err, errUnreadable) {
		if rerr := store.Remove(context.WithoutCancel(ctx), j.Key); rerr != nil && err == nil {
			return fmt.Errorf("unpack: remove archive: %w", rerr)
		}
	}
	return err
}

// download copies an archive to a temp file: zip.NewReader needs to read at any offset.
func download(ctx context.Context, store *storage.Store, key string) (*os.File, int64, error) {
	obj, err := store.Get(ctx, key)
	if err != nil {
		return nil, 0, fmt.Errorf("unpack: fetch archive: %w", err)
	}
	defer obj.Close()
	f, err := os.CreateTemp("", "unpack-*.zip")
	if err != nil {
		return nil, 0, fmt.Errorf("unpack: temp file: %w", err)
	}
	size, err := io.Copy(f, obj)
	if err != nil {
		f.Close()
		os.Remove(f.Name()) //nolint:errcheck // a temp file
		return nil, 0, fmt.Errorf("unpack: fetch archive: %w", err)
	}
	return f, size, nil
}

func walk(ctx context.Context, pool *pgxpool.Pool, jobID int64, j *Job, ra io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreadable, err)
	}
	w := &walker{job: j, resumeFrom: j.State.Cursor}
	w.enq = &ingest.Enqueuer{
		Pool:   pool,
		UserID: j.UserID,
		Done: func(_ int, res ingest.Enqueued) {
			if res.AlreadyProcessed {
				w.job.State.Already++
			}
		},
		Commit: func(ctx context.Context, tx pgx.Tx) error {
			return saveState(ctx, tx, jobID, w.job.State, w.pos)
		},
	}
	switch j.Format {
	case FormatTakeout:
		err = w.takeout(ctx, zr)
	default:
		err = w.zip(ctx, zr)
	}
	if err != nil {
		return err
	}
	if err := w.enq.Flush(ctx); err != nil {
		return err
	}
	return saveState(ctx, pool, jobID, w.job.State, w.pos)
}

// saveState records a job's progress in its own payload.
func saveState(ctx context.Context, db ingest.Querier, jobID int64, st State, cursor int) error {
	st.Cursor = cursor
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("unpack: marshal state: %w", err)
	}
	if _, err := db.Exec(ctx, `UPDATE jobs SET payload = jsonb_set(payload, '{state}', $2::jsonb) WHERE id = $1`, jobID, string(b)); err != nil {
		return fmt.Errorf("unpack: save state: %w", err)
	}
	return nil
}

// walker goes through an archive's units in an order that's the same on every run, skipping
// those a cut-off earlier run already saved.
type walker struct {
	job        *Job
	enq        *ingest.Enqueuer
	pos        int // units passed so far
	resumeFrom int
}

// next moves past one unit and reports whether it still needs dealing with.
func (w *walker) next() bool {
	w.pos++
	return w.pos > w.resumeFrom
}

func (w *walker) add(ctx context.Context, it ingest.RawItem) error {
	it.Batch, it.BatchTitle = w.job.Batch, w.job.BatchTitle
	return w.enq.Add(ctx, it)
}

// zip enqueues each .gpx/.fit/.tcx entry of a plain archive — the same job a plain upload of
// that file gets, so a file is handled identically whether it arrived alone or inside an
// archive.
func (w *walker) zip(ctx context.Context, zr *zip.Reader) error {
	entries := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if entries >= MaxEntries {
			w.job.State.Truncated = true
			break
		}
		entries++
		if !w.next() {
			continue
		}

		name := filepath.Base(f.Name) // strip any directory structure inside the archive
		ext := strings.ToLower(filepath.Ext(name))
		// The declared size is checked before decompressing anything — the cheap half of the
		// zip-bomb defense. The read below is bounded independently, since a declared size is
		// exactly what a hostile or corrupt archive can lie about.
		if !AllowedExt[ext] || f.UncompressedSize64 > MaxEntryBytes {
			w.job.State.Skipped++
			continue
		}
		data, err := readEntry(f)
		if err != nil || len(data) == 0 || len(data) > MaxEntryBytes {
			w.job.State.Skipped++
			continue
		}
		if err := w.add(ctx, ingest.RawItem{Source: "upload", Filename: name, Ext: ext, Data: data}); err != nil {
			return err
		}
	}
	return nil
}

// readEntry reads one archive entry, bounded to one byte more than MaxEntryBytes, so a read
// that reaches it is known to be oversized.
func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, MaxEntryBytes+1))
}

// takeout enqueues a Takeout export's activities. It has no standalone activity files to walk:
// its GPS and exercise data live in separate CSV/JSON families that internal/takeout joins.
// One activity type at a time (takeout.Archive.Extract says why), each activity as the GPX
// internal/takeout writes, with the type passed as the job's ActivityType — a bare
// per-activity GPX carries no `<type>` for the parser to read back. Activities go in file-name
// order within their type.
func (w *walker) takeout(ctx context.Context, zr *zip.Reader) error {
	archive, err := takeout.Open(zr)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreadable, err)
	}
	for _, t := range archive.Types() {
		// A swim with no coordinates is not something anyone can hand over as a track. These
		// logs' own distance and duration are a separate, not-yet-built import path — see
		// IMPLEMENTATION.md's Takeout section; their calories and heart rate never will be
		// (VISION.md §1.1).
		if t.WithGPS == 0 {
			continue
		}
		activities, err := archive.Extract(t.Name)
		if err != nil || len(activities) == 0 {
			// Logs that claim GPS but whose day files are missing or empty over their window —
			// the join isn't total in real exports either. One skipped unit for the type.
			if w.next() {
				w.job.State.Skipped++
			}
			continue
		}
		names := takeout.FileNames(activities)
		order := make([]int, len(activities))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return names[order[a]] < names[order[b]] })
		for _, i := range order {
			if !w.next() {
				continue
			}
			if err := w.add(ctx, ingest.RawItem{
				Source: "takeout", Filename: names[i], Ext: ".gpx", ActivityType: t.Name, Data: activities[i].GPX(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
