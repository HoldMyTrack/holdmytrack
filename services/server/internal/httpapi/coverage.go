package httpapi

import (
	"net/http"
)

// coverageRenderingQuery reports whether any job that changes this account's Fog/Heatmap
// rasters is still unfinished. A claimed job keeps state = 'pending' until the worker marks it
// done or failed (internal/worker's claimAndRunOne only sets locked_at), so this covers
// running jobs too. `ingest`, `edit_track` and `reprivacy` are included, not just `render_fog`: an upload
// the client has already seen accepted may not have been parsed yet, and until it has, the
// `render_fog` job it will enqueue doesn't exist to be waited on.
//
// The second column is the version: when any of the account's Fog/Heatmap tiles was last
// written, in Unix milliseconds (0 before the first). A job can render more than once before it
// finishes — a track edit or Private location change renders once with its activities left out
// (Pending) and again once they're back — so a client that should show the in-between state
// refetches whenever this changes, not only once rendering is over.
const coverageRenderingQuery = `
SELECT EXISTS (
    SELECT 1 FROM jobs
    WHERE user_id = $1 AND state = 'pending' AND kind IN ('ingest', 'edit_track', 'reprivacy', 'render_fog')
),
COALESCE((SELECT (EXTRACT(EPOCH FROM MAX(rendered_at)) * 1000)::bigint FROM fog_tiles WHERE user_id = $1), 0)`

type coverageStatusResponse struct {
	Rendering bool  `json:"rendering"`
	Version   int64 `json:"version"`
}

// handleCoverageStatus serves `GET /v1/coverage/status` (IMPLEMENTATION.md §4.2.3): the
// signal a client polls after an upload or delete to know when refetching Fog/Heatmap tiles
// will return the new coverage rather than the old. The tile URLs themselves are fixed and
// the rasters are rendered asynchronously, so without this the client has no way to tell.
func (s *Server) handleCoverageStatus(w http.ResponseWriter, r *http.Request) {
	var resp coverageStatusResponse
	if err := s.pool.QueryRow(r.Context(), coverageRenderingQuery, userIDFromContext(r.Context())).Scan(&resp.Rendering, &resp.Version); err != nil {
		s.log.Error("coverage status query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}
