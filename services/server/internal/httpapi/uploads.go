package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

// defaultUploadsLimit matches the upload panel's own page size — 5 rows at a time.
const defaultUploadsLimit = 5

// maxUploadsLimit is a sanity ceiling on the page size a caller can ask for, not a claim that
// this user's real history is ever large enough to need it — see uploadsCountQuery's own
// comment on why LIMIT/OFFSET is fine here despite §4.7's list endpoint dropping pagination.
const maxUploadsLimit = 50

// uploadRow is one entry in the persistent upload history — IMPLEMENTATION.md
// §4.0.1's "the more valuable of the two" feature, distinct from live in-flight status
// (handleActivityStatus): this answers "what happened to every upload ever, including ones
// whose tab is long closed," which nothing before this endpoint could answer at all.
type uploadRow struct {
	Filename    string    `json:"filename"`
	ExternalID  string    `json:"external_id"`
	Status      string    `json:"status"` // "processing" | "done" | "failed"
	Error       string    `json:"error,omitempty"`
	SubmittedAt time.Time `json:"submitted_at"`
	// Populated only when Status == "done" — the frontend shows a finished
	// row as "READY · 11 Sep · 8.2 km" rather than repeating the filename's own status.
	StartedAt      *time.Time `json:"started_at,omitempty"`
	DistanceMeters *float64   `json:"distance_meters,omitempty"`
}

type uploadsResponse struct {
	Total int64 `json:"total"`
	// A global count (every pending ingest job for this user, not just this page) — the
	// panel's "N in progress" badge needs the true number regardless of which page happens
	// to be showing, not an approximation from whatever page a caller is currently viewing.
	Processing int64       `json:"processing"`
	Limit      int         `json:"limit"`
	Offset     int         `json:"offset"`
	Uploads    []uploadRow `json:"uploads"`
}

// uploadsCountQuery and uploadsListQuery read the `jobs` table directly rather than a
// dedicated uploads table — every upload already is an `ingest` job (handleUpload,
// handleZipUpload), and `jobs` already carries everything a history row needs (filename via
// `payload->>'source_detail'`, external_id, state, last_error, created_at). LIMIT/OFFSET,
// not the keyset pagination §4.7's histogram uses, because this is the one place in the
// upload flow §4.0.1 says pagination is actually justified — unlike the activity list, an
// upload log grows monotonically forever and a caller legitimately wants "page 3", not
// "everything since some cursor."
const uploadsCountQuery = `SELECT COUNT(*) FROM jobs WHERE kind = 'ingest' AND user_id = $1`

const uploadsProcessingCountQuery = `SELECT COUNT(*) FROM jobs WHERE kind = 'ingest' AND user_id = $1 AND state = 'pending'`

// The LEFT JOIN is what turns a "done" job into a "9 Sep · 34.7 km" row instead of just a
// bare status. Joined on `a.source = j.payload->>'source'`, not a literal 'upload' — a plain
// upload and a Google Takeout import (handleTakeoutUpload, source = 'takeout') are both
// `kind = 'ingest'` jobs this query already selects, and each one's own persisted activity
// carries the matching source, not always 'upload'. A hardcoded literal here quietly left
// every Takeout-imported row showing "Ready" with no date or distance, since the join never
// matched — caught live by opening the panel after a real Takeout import, not a unit test.
const uploadsListQuery = `
SELECT j.payload->>'source_detail' AS filename,
       j.payload->>'external_id' AS external_id,
       j.state, j.last_error, j.created_at,
       a.started_at, a.distance_meters
FROM jobs j
LEFT JOIN activities a
  ON a.user_id = j.user_id AND a.source = j.payload->>'source' AND a.external_id = j.payload->>'external_id'
WHERE j.kind = 'ingest' AND j.user_id = $1
ORDER BY j.id DESC
LIMIT $2 OFFSET $3`

// handleListUploads serves §4.0.1's `GET /v1/uploads?limit=&offset=`.
func (s *Server) handleListUploads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := defaultUploadsLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, `invalid "limit", want a positive integer`, http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > maxUploadsLimit {
		limit = maxUploadsLimit
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, `invalid "offset", want a non-negative integer`, http.StatusBadRequest)
			return
		}
		offset = n
	}

	ctx := r.Context()
	userID := userIDFromContext(ctx)

	var total, processing int64
	if err := s.pool.QueryRow(ctx, uploadsCountQuery, userID).Scan(&total); err != nil {
		s.log.Error("uploads count query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.pool.QueryRow(ctx, uploadsProcessingCountQuery, userID).Scan(&processing); err != nil {
		s.log.Error("uploads processing-count query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, err := s.pool.Query(ctx, uploadsListQuery, userID, limit, offset)
	if err != nil {
		s.log.Error("uploads list query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	uploads := make([]uploadRow, 0)
	for rows.Next() {
		var u uploadRow
		var state string
		var lastError *string
		if err := rows.Scan(&u.Filename, &u.ExternalID, &state, &lastError, &u.SubmittedAt, &u.StartedAt, &u.DistanceMeters); err != nil {
			s.log.Error("uploads scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Same three-way mapping as handleActivityStatus, duplicated rather than shared:
		// that function reads one job by external_id, this one reads a page of jobs, and
		// factoring out a two-line switch across both would cost more to read than it saves.
		switch state {
		case "pending":
			u.Status = "processing"
		case "done":
			u.Status = "done"
		case "failed":
			u.Status = "failed"
			if lastError != nil {
				u.Error = *lastError
			}
		default:
			u.Status = state
		}
		uploads = append(uploads, u)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("uploads read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, uploadsResponse{Total: total, Processing: processing, Limit: limit, Offset: offset, Uploads: uploads})
}
