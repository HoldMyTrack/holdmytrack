package httpapi

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/unpack"
)

// archiveUploadResponse is what an archive upload is answered with as soon as it has arrived:
// its files are read by the worker's `unpack` job, and what became of them — already
// imported, skipped, more than one upload reads — reaches the Upload menu once that's done
// (GET /v1/uploads/active's `unpacked`, matched by Batch).
type archiveUploadResponse struct {
	Status   string `json:"status"` // "zip_accepted"
	Filename string `json:"filename"`
	Batch    string `json:"batch"`
}

// handleZipUpload is handleUpload's branch for a `.zip` (IMPLEMENTATION.md §4.0.1's bulk
// import case, ADR-0032): a plain archive of .gpx/.fit/.tcx files, or a Google Takeout export.
// It stores the archive once and enqueues one `unpack` job, which walks it under §5.1's
// zip-bomb bounds and enqueues an `ingest` job per activity (internal/unpack). The request
// tells the two kinds apart from the central directory alone, which zr has already read.
//
// The caller has opened the archive as a *zip.Reader over the uploaded part, which the
// multipart part (a temp file for anything large) can serve and a bare HTTP body can't; the
// archive is stored from the same part, without a second copy.
func (s *Server) handleZipUpload(w http.ResponseWriter, r *http.Request, zr *zip.Reader, ra io.ReaderAt, size int64, filename string) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	batch := newBatchID()
	if batch == "" {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	job := unpack.Job{UserID: userID, Source: "upload", Format: unpack.FormatZip, Key: unpack.Key(userID, batch), Batch: batch, BatchTitle: filename}
	if unpack.IsTakeout(zr) {
		job.Source, job.Format = "takeout", unpack.FormatTakeout
	}
	payload, err := json.Marshal(job)
	if err != nil {
		s.log.Error("unpack job marshal failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.store.Put(ctx, job.Key, io.NewSectionReader(ra, 0, size), size); err != nil {
		s.log.Error("archive store failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO jobs (kind, user_id, payload) VALUES ('unpack', $1, $2)`, userID, payload); err != nil {
		s.log.Error("unpack enqueue failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, archiveUploadResponse{Status: "zip_accepted", Filename: filename, Batch: batch})
}
