package httpapi

import (
	"net/http"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// activeImport is one row of the header's Upload menu (IMPLEMENTATION.md §4.0.1): an import
// still being processed — a single uploaded file, or every job one request enqueued together
// (a .zip's or Takeout export's files, a phone sync's activities, ingest.Job.Batch), with how
// many of them have finished. A row leaves the list once all of its jobs have; what they came
// to is the /sync page's.
type activeImport struct {
	Title       string    `json:"title"`
	Source      string    `json:"source"`
	Done        int64     `json:"done"`
	Total       int64     `json:"total"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type activeImportsResponse struct {
	Imports []activeImport `json:"imports"`
	// Failed imports that finished after the account last looked (users.imports_seen_at, moved
	// by opening /sync or by POST /v1/uploads/seen) — the Upload menu's "1 import failed" line.
	UnseenFailures int64 `json:"unseen_failures"`
}

// activeImportsQuery groups the account's ingest jobs by batch — a job with none is a group of
// its own — and keeps the groups that still have a pending job. The title prefers the batch's
// own (an archive's name), then the job's file name; a phone sync has neither worth showing
// (its source_detail is a platform id), so importTitle names it after its source instead.
const activeImportsQuery = `
WITH grouped AS (
  SELECT COALESCE(NULLIF(payload->>'batch', ''), id::text) AS grp, payload, state, created_at
  FROM jobs
  WHERE kind = 'ingest' AND user_id = $1
)
SELECT MIN(payload->>'source'),
       COALESCE(MIN(NULLIF(payload->>'batch_title', '')), ''),
       COALESCE(MIN(payload->>'source_detail'), ''),
       COUNT(*) FILTER (WHERE state <> 'pending'),
       COUNT(*),
       MIN(created_at)
FROM grouped
WHERE grp IN (SELECT grp FROM grouped WHERE state = 'pending')
GROUP BY grp
ORDER BY MIN(created_at) DESC`

const unseenImportFailuresQuery = `
SELECT COUNT(*) FROM jobs j JOIN users u ON u.id = j.user_id
WHERE j.kind = 'ingest' AND j.user_id = $1 AND j.state = 'failed' AND j.finished_at > u.imports_seen_at`

// importTitle is what an import is called wherever a person sees it — the Upload menu and the
// /sync page: an uploaded file's or archive's name, or, for a phone sync, its source, since the
// job's own file name there is a platform id never meant to be read.
func importTitle(l *i18n.Localizer, source, batchTitle, filename string) string {
	switch {
	case batchTitle != "":
		return batchTitle
	case source == "upload" || source == "takeout":
		return filename
	default:
		return sourceLabel(l, source)
	}
}

// sourceLabel names an import source: "Health Connect", "GPS Logger", "Google Takeout"…
func sourceLabel(l *i18n.Localizer, source string) string {
	switch source {
	case "upload", "takeout", "healthconnect", "healthkit", "recorded":
		return l.T("imports.source." + source)
	default:
		return source
	}
}

// handleActiveUploads serves `GET /v1/uploads/active`: the imports still being processed, and
// how many failures the account hasn't seen yet.
func (s *Server) handleActiveUploads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	l := i18n.Get(requestLang(r))

	rows, err := s.pool.Query(ctx, activeImportsQuery, userID)
	if err != nil {
		s.log.Error("active imports query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	resp := activeImportsResponse{Imports: make([]activeImport, 0)}
	for rows.Next() {
		var a activeImport
		var batchTitle, filename string
		if err := rows.Scan(&a.Source, &batchTitle, &filename, &a.Done, &a.Total, &a.SubmittedAt); err != nil {
			s.log.Error("active imports scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		a.Title = importTitle(l, a.Source, batchTitle, filename)
		resp.Imports = append(resp.Imports, a)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("active imports read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.pool.QueryRow(ctx, unseenImportFailuresQuery, userID).Scan(&resp.UnseenFailures); err != nil {
		s.log.Error("unseen import failures query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// markImportsSeen moves the account's imports_seen_at to now: every failure so far has been
// seen. Opening /sync does it, and so does dismissing the Upload menu's failure line.
func (s *Server) markImportsSeen(r *http.Request, userID string) error {
	_, err := s.pool.Exec(r.Context(), `UPDATE users SET imports_seen_at = NOW() WHERE id = $1`, userID)
	return err
}

// handleImportsSeen serves `POST /v1/uploads/seen` — the Upload menu's dismiss on its failure line.
func (s *Server) handleImportsSeen(w http.ResponseWriter, r *http.Request) {
	if err := s.markImportsSeen(r, userIDFromContext(r.Context())); err != nil {
		s.log.Error("mark imports seen failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newBatchID names one request's jobs as a batch (ingest.Job.Batch). An empty id — random bytes
// unavailable — only means those jobs show as separate rows in the Upload menu.
func newBatchID() string {
	id, err := randomToken()
	if err != nil {
		return ""
	}
	return id
}
