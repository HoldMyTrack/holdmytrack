package httpapi

import (
	"net/http"
)

// coverageRenderingQuery reports whether any job that changes this account's Fog/Heatmap
// rasters is still unfinished. A claimed job keeps state = 'pending' until the worker marks it
// done or failed (internal/worker's claimAndRunOne only sets locked_at), so this covers
// running jobs too. `ingest` and `edit_track` are included, not just `render_fog`: an upload
// the client has already seen accepted may not have been parsed yet, and until it has, the
// `render_fog` job it will enqueue doesn't exist to be waited on.
const coverageRenderingQuery = `
SELECT EXISTS (
    SELECT 1 FROM jobs
    WHERE user_id = $1 AND state = 'pending' AND kind IN ('ingest', 'edit_track', 'render_fog')
)`

type coverageStatusResponse struct {
	Rendering bool `json:"rendering"`
}

// handleCoverageStatus serves `GET /v1/coverage/status` (IMPLEMENTATION.md §4.2.3): the
// signal a client polls after an upload or delete to know when refetching Fog/Heatmap tiles
// will return the new coverage rather than the old. The tile URLs themselves are fixed and
// the rasters are rendered asynchronously, so without this the client has no way to tell.
func (s *Server) handleCoverageStatus(w http.ResponseWriter, r *http.Request) {
	var resp coverageStatusResponse
	if err := s.pool.QueryRow(r.Context(), coverageRenderingQuery, userIDFromContext(r.Context())).Scan(&resp.Rendering); err != nil {
		s.log.Error("coverage status query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}
