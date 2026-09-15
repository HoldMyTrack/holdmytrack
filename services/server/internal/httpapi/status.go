package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type statusResponse struct {
	Status string `json:"status"` // "processing" | "done" | "failed" | "unknown"
	Error  string `json:"error,omitempty"`
}

// handleActivityStatus lets the frontend find out what handleUpload's "enqueued" response
// actually resolved to. That response is necessarily optimistic — the ingest job runs
// asynchronously in cmd/fitmap work — so without this, a failed job (a GPX with no track
// points, say) looked identical in the UI to a successful one: the widget just said
// "enqueued" and the map silently never updated, with no signal that anything went wrong.
//
// Looks at the most recent ingest job for this external_id, not the activities table
// directly, so "failed" is distinguishable from "never existed" — a failed job never
// persists an activities row, so that table alone can't tell the two apart.
func (s *Server) handleActivityStatus(w http.ResponseWriter, r *http.Request) {
	externalID := r.PathValue("external_id")
	if externalID == "" {
		http.Error(w, "missing external_id", http.StatusBadRequest)
		return
	}

	var state string
	var lastError *string
	err := s.pool.QueryRow(r.Context(),
		`SELECT state, last_error FROM jobs
		 WHERE kind = 'ingest' AND payload->>'external_id' = $1
		 ORDER BY id DESC LIMIT 1`,
		externalID,
	).Scan(&state, &lastError)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, statusResponse{Status: "unknown"})
			return
		}
		s.log.Error("status query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := statusResponse{}
	switch state {
	case "pending":
		resp.Status = "processing"
	case "done":
		resp.Status = "done"
	case "failed":
		resp.Status = "failed"
		if lastError != nil {
			resp.Error = *lastError
		}
	default:
		resp.Status = state // forward-compatible with any future jobs.state value
	}
	writeJSON(w, http.StatusOK, resp)
}
