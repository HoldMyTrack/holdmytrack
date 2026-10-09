package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// maxOverlapSpans bounds one `POST /v1/activities/overlaps` request: what the Android Sync tab
// lists, three months of a phone's sessions plus its recordings, is a few hundred at most.
const maxOverlapSpans = 5000

// maxOverlapKeyLen bounds a span's key, which only ever comes back to the caller.
const maxOverlapKeyLen = 256

type overlapsRequest struct {
	Spans []struct {
		Key   string    `json:"key"`
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	} `json:"spans"`
}

type overlapJSON struct {
	Key          string    `json:"key"`
	ActivityID   string    `json:"activity_id"`
	Name         string    `json:"name"`
	ActivityType string    `json:"activity_type"`
	StartedAt    time.Time `json:"started_at"`
	Timezone     *string   `json:"timezone"`
}

type overlapsResponse struct {
	Overlaps []overlapJSON `json:"overlaps"`
}

// handleActivityOverlaps answers, for each time span a client is about to offer for import,
// the live activity it overlaps (§4.6, SPEC FR-3.7): the hint that ticking it would make a
// second copy of something the account already has. It decides nothing; the spans not listed
// overlap nothing.
func (s *Server) handleActivityOverlaps(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSyncBodyBytes)
	var req overlapsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Spans) > maxOverlapSpans {
		http.Error(w, fmt.Sprintf("too many spans (%d), want %d or fewer", len(req.Spans), maxOverlapSpans), http.StatusBadRequest)
		return
	}
	spans := make([]ingest.Span, 0, len(req.Spans))
	for _, sp := range req.Spans {
		if sp.Key == "" || len(sp.Key) > maxOverlapKeyLen {
			http.Error(w, fmt.Sprintf(`invalid "key", want 1 to %d characters`, maxOverlapKeyLen), http.StatusBadRequest)
			return
		}
		spans = append(spans, ingest.Span{Key: sp.Key, Start: sp.Start, End: sp.End})
	}
	ctx := r.Context()
	found, err := ingest.Overlaps(ctx, s.pool, userIDFromContext(ctx), spans)
	if err != nil {
		s.log.Error("activity overlaps failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := overlapsResponse{Overlaps: make([]overlapJSON, 0, len(found))}
	for _, o := range found {
		resp.Overlaps = append(resp.Overlaps, overlapJSON{
			Key: o.Key, ActivityID: o.ActivityID, Name: o.Name, ActivityType: o.ActivityType,
			StartedAt: o.StartedAt, Timezone: o.Timezone,
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}
