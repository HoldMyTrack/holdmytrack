package httpapi

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
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
// contained .gpx/.fit/.tcx file, via the exact same persistAndEnqueue a plain single-file
// upload uses, so a file that happens to arrive inside a zip is treated no differently once
// extracted than one dropped on its own. A Google Takeout export is a `.zip` too but takes a
// different path entirely — see isTakeoutArchive and handleTakeoutUpload in
// takeout_upload.go — since it has no standalone .gpx/.fit/.tcx entries to walk this way at
// all.
//
// The caller (handleUpload) has already read the archive fully into memory and opened it as
// a *zip.Reader — zip.NewReader needs an io.ReaderAt, which an HTTP body doesn't provide, so
// there is no streaming alternative here the way ingest.Process manages for a single file.
// This function owns everything from there: walking entries under §5.1's zip-bomb defenses.
func (s *Server) handleZipUpload(w http.ResponseWriter, r *http.Request, zr *zip.Reader, filename string) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	results := make([]zipEntryResult, 0, len(zr.File))
	processed := 0
	truncated := false

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

		externalID, alreadyProcessed, err := s.persistAndEnqueue(ctx, uploadFileParams{
			UserID: userID, Source: "upload", Filename: entryName, Ext: ext, Data: entryData,
		})
		if err != nil {
			s.log.Error("zip entry persist/enqueue failed", "err", err, "entry", entryName)
			results = append(results, zipEntryResult{Filename: entryName, Status: "skipped", Reason: "internal error"})
			continue
		}
		status := "enqueued"
		if alreadyProcessed {
			status = "already_processed"
		}
		results = append(results, zipEntryResult{Filename: entryName, Status: status, ExternalID: externalID})
	}

	writeJSON(w, http.StatusAccepted, zipUploadResponse{
		Status: "zip_processed", Filename: filename, Files: results, Truncated: truncated,
	})
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
