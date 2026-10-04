package httpapi

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// zipEntryResult is one contained file's outcome — the same three states a plain single-file
// upload can reach (uploadResponse's "enqueued"/"already_processed"), plus "skipped" for a
// contained file this server declined to process. §5.1's "one bad file in a bulk import
// cannot abort the batch" is exactly why this is a list of per-entry outcomes rather than one
// request failing or succeeding as a whole.
type zipEntryResult struct {
	Filename   string `json:"filename"`
	Status     string `json:"status"` // "enqueued" | "already_processed" | "skipped"
	ExternalID string `json:"external_id,omitempty"`
	// Only set when Status == "skipped" — why this one entry didn't become a job.
	Reason string `json:"reason,omitempty"`
}

type zipUploadResponse struct {
	Status   string           `json:"status"` // "zip_processed"
	Filename string           `json:"filename"`
	Files    []zipEntryResult `json:"files"`
	// True when the archive had more non-directory entries than maxZipEntries — the ones
	// past the cap were never looked at, not merely skipped for a per-file reason.
	Truncated bool `json:"truncated,omitempty"`
}

// handleZipUpload is handleUpload's branch for a plain `.zip` archive
// (IMPLEMENTATION.md §4.0.1's bulk-historical-import case) — one job enqueued per
// contained .gpx/.fit/.tcx file, via the same ingest.EnqueueRaw a plain single-file
// upload uses (a chunk of entries at a time, ingest.Enqueuer), so a file that happens to arrive inside a zip is treated no differently once
// extracted than one dropped on its own. A Google Takeout export is a `.zip` too but takes a
// different path entirely — see isTakeoutArchive and handleTakeoutUpload in
// takeout_upload.go — since it has no standalone .gpx/.fit/.tcx entries to walk this way at
// all.
//
// The caller (handleUpload) has already opened the archive as a *zip.Reader over the uploaded
// part — zip.NewReader needs an io.ReaderAt, which the multipart part (a temp file for
// anything large) provides and a bare HTTP body doesn't. This function owns everything from
// there: walking entries under §5.1's zip-bomb defenses.
func (s *Server) handleZipUpload(w http.ResponseWriter, r *http.Request, zr *zip.Reader, filename string) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	results := make([]zipEntryResult, 0, len(zr.File))
	batch := newBatchID()
	processed := 0
	truncated := false
	var slots []int // results index of each enqueued entry
	enq := &ingest.Enqueuer{Pool: s.pool, UserID: userID, Done: func(seq int, res ingest.Enqueued, err error) {
		setEntryResult(&results[slots[seq]], res, err, s.log)
	}}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if processed >= maxZipEntries {
			truncated = true
			break
		}
		processed++

		entryName := filepath.Base(f.Name) // strip any directory structure inside the archive
		ext := strings.ToLower(filepath.Ext(entryName))
		if !allowedExt[ext] {
			results = append(results, zipEntryResult{
				Filename: entryName, Status: "skipped",
				Reason: fmt.Sprintf("unsupported file type %q", ext),
			})
			continue
		}
		// Checked against the archive's own declared size before decompressing anything —
		// the cheap half of the zip-bomb defense. The read below is bounded independently,
		// since a declared size is exactly the kind of thing a hostile or corrupt archive
		// can lie about.
		if f.UncompressedSize64 > maxZipEntryBytes {
			results = append(results, zipEntryResult{Filename: entryName, Status: "skipped", Reason: "file too large"})
			continue
		}

		entryData, err := readZipEntry(f)
		if err != nil {
			s.log.Error("zip entry read failed", "err", err, "entry", entryName)
			results = append(results, zipEntryResult{Filename: entryName, Status: "skipped", Reason: "could not read entry"})
			continue
		}
		if len(entryData) == 0 {
			results = append(results, zipEntryResult{Filename: entryName, Status: "skipped", Reason: "empty file"})
			continue
		}
		if len(entryData) > maxZipEntryBytes {
			results = append(results, zipEntryResult{Filename: entryName, Status: "skipped", Reason: "file too large"})
			continue
		}

		results = append(results, zipEntryResult{Filename: entryName})
		slots = append(slots, len(results)-1)
		enq.Add(ctx, ingest.RawItem{
			Source: "upload", Filename: entryName, Ext: ext, Data: entryData,
			Batch: batch, BatchTitle: filename,
		})
	}

	enq.Flush(ctx)

	writeJSON(w, http.StatusAccepted, zipUploadResponse{
		Status: "zip_processed", Filename: filename, Files: results, Truncated: truncated,
	})
}

// setEntryResult fills in an archive entry's result once its chunk has been enqueued.
func setEntryResult(r *zipEntryResult, res ingest.Enqueued, err error, log *slog.Logger) {
	switch {
	case err != nil:
		log.Error("archive entry enqueue failed", "err", err, "file", r.Filename)
		r.Status, r.Reason = "skipped", "internal error"
	case res.AlreadyProcessed:
		r.Status, r.ExternalID = "already_processed", res.ExternalID
	default:
		r.Status, r.ExternalID = "enqueued", res.ExternalID
	}
}

// readZipEntry opens and fully reads one archive entry, bounded to one more byte than
// maxZipEntryBytes allows — the caller treats a read that actually hits that ceiling as
// oversized (the declared UncompressedSize64 was already checked before this is even
// called, but a corrupt or adversarial archive can still decompress to more than it claims).
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxZipEntryBytes+1))
}
