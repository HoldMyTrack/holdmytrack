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
	Title  string `json:"title"`
	Source string `json:"source"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	// Unpacking is an archive whose `unpack` job is still finding its files (internal/unpack):
	// Total counts only those found so far.
	Unpacking   bool      `json:"unpacking"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// unpackedArchive is an archive whose `unpack` job finished in the last unpackedWindow: what
// its files came to beyond the ingest jobs it made, which the client that uploaded it shows as
// notes, matching Batch against its upload's response.
type unpackedArchive struct {
	Batch     string `json:"batch"`
	Title     string `json:"title"`
	Already   int    `json:"already"`
	Skipped   int    `json:"skipped"`
	Truncated bool   `json:"truncated"`
	// Error, when the job failed, is why, in the request's language.
	Error string `json:"error,omitempty"`
}

// unpackedWindow is how long a finished archive stays in `unpacked`: far longer than a client
// polling every few seconds needs, short enough that the list stays small.
const unpackedWindow = time.Hour

type activeImportsResponse struct {
	Imports  []activeImport    `json:"imports"`
	Unpacked []unpackedArchive `json:"unpacked"`
	// Failed imports that finished after the account last looked (users.imports_seen_at, moved
	// by opening /sync) — the red dot on the header's Sync item.
	UnseenFailures int64 `json:"unseen_failures"`
}

// activeImportsQuery groups the account's ingest jobs by batch — a job with none is a group of
// its own — and keeps the groups that still have a pending job. An archive's `unpack` job
// carries its batch too, so an archive is one row from the moment it's uploaded, unpacking and
// then processing; only ingest jobs are counted. The title prefers the batch's own (an
// archive's name), then the job's file name; a phone sync has neither worth showing (its
// source_detail is a platform id), so importTitle names it after its source instead.
const activeImportsQuery = `
WITH grouped AS (
  SELECT COALESCE(NULLIF(payload->>'batch', ''), id::text) AS grp, kind, payload, state, created_at
  FROM jobs
  WHERE kind IN ('ingest', 'unpack') AND user_id = $1
)
SELECT MIN(payload->>'source'),
       COALESCE(MIN(NULLIF(payload->>'batch_title', '')), ''),
       COALESCE(MIN(payload->>'source_detail'), ''),
       COUNT(*) FILTER (WHERE kind = 'ingest' AND state <> 'pending'),
       COUNT(*) FILTER (WHERE kind = 'ingest'),
       bool_or(kind = 'unpack' AND state = 'pending'),
       MIN(created_at)
FROM grouped
WHERE grp IN (SELECT grp FROM grouped WHERE state = 'pending')
GROUP BY grp
ORDER BY MIN(created_at) DESC`

// unpackedQuery is the account's archives whose unpack job finished within unpackedWindow.
const unpackedQuery = `
SELECT payload->>'batch', COALESCE(payload->>'batch_title', ''), state, COALESCE(error_code, ''),
       COALESCE((payload->'state'->>'already')::int, 0), COALESCE((payload->'state'->>'skipped')::int, 0),
       COALESCE((payload->'state'->>'truncated')::boolean, false)
FROM jobs
WHERE user_id = $1 AND kind = 'unpack' AND state IN ('done', 'failed')
  AND finished_at > NOW() - make_interval(secs => $2)
ORDER BY finished_at DESC`

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
	case "upload", "takeout", "healthconnect", "healthkit", "recorded", "timeline":
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
	resp := activeImportsResponse{Imports: make([]activeImport, 0), Unpacked: make([]unpackedArchive, 0)}
	for rows.Next() {
		var a activeImport
		var batchTitle, filename string
		if err := rows.Scan(&a.Source, &batchTitle, &filename, &a.Done, &a.Total, &a.Unpacking, &a.SubmittedAt); err != nil {
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
	if resp.Unpacked, err = s.unpackedArchives(r, l, userID); err != nil {
		s.log.Error("unpacked archives query failed", "err", err)
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

// unpackedArchives is the response's `unpacked`.
func (s *Server) unpackedArchives(r *http.Request, l *i18n.Localizer, userID string) ([]unpackedArchive, error) {
	rows, err := s.pool.Query(r.Context(), unpackedQuery, userID, unpackedWindow.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]unpackedArchive, 0)
	for rows.Next() {
		var a unpackedArchive
		var state, code string
		if err := rows.Scan(&a.Batch, &a.Title, &state, &code, &a.Already, &a.Skipped, &a.Truncated); err != nil {
			return nil, err
		}
		if state == "failed" {
			a.Error = l.T("unpack_error." + code)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// markImportsSeen moves the account's imports_seen_at to now: every failure so far has been
// seen. Opening /sync does it.
func (s *Server) markImportsSeen(r *http.Request, userID string) error {
	_, err := s.pool.Exec(r.Context(), `UPDATE users SET imports_seen_at = NOW() WHERE id = $1`, userID)
	return err
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
