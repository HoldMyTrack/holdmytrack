package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// syncSources is the `source` allowlist this endpoint accepts — IMPLEMENTATION.md §4.0 names
// the two on-device platforms for Path 2 (iOS/HealthKit, Android/Health Connect), and §4.0.4
// adds "recorded" for in-app GPS recording (ADR-0007) — authored by HoldMyTrack itself rather than
// read from a platform health store, but the same batched wire shape either way. Path 1
// (webhooks) and Path 3 (upload) have their own endpoints and their own `source` values, so
// this list doesn't need to anticipate those.
var syncSources = map[string]bool{"healthconnect": true, "healthkit": true, "recorded": true}

// maxSyncBatchActivities bounds one request to a reasonable page of a foreground sync run, not
// a claim that a real sync history can't be larger — ROADMAP.md's own "resumable retry"
// requirement (§4.0: "mobile uploads get interrupted... retries are normal") already assumes a
// first backfill makes several requests rather than one unbounded one.
const maxSyncBatchActivities = 100

// maxSyncPointsPerActivity matches parse.go's own "100-mile ride at 1 Hz is 36,000+ points"
// ceiling for file formats — a synced activity carries the same real-world variance whether it
// arrived as GPX or as batched JSON points.
const maxSyncPointsPerActivity = 50000

// maxSyncBodyBytes bounds the whole batched request body: generously above
// maxSyncBatchActivities activities at maxSyncPointsPerActivity points each, the same
// "cap before reading the body fully" posture handleUpload's maxUploadBytes documents.
const maxSyncBodyBytes = 64 << 20 // 64 MiB

// syncActivityRequest is one activity in the batch. parse.JSONActivity is embedded rather than
// duplicated so the request body's own wire shape and the raw payload persisted to object
// storage (re-marshaled from the same embedded value, see handleSyncActivities) are guaranteed
// identical — there is exactly one place that defines what "activity_type" and "points" mean
// on the wire.
type syncActivityRequest struct {
	// ExternalID is the platform health store's own stable id for this record (a Health
	// Connect session UUID, eventually a HealthKit workout UUID) — the actual identity
	// (user_id, source, external_id) idempotency keys off, not a hash of the bytes below,
	// so a retried sync of the same record is recognized as the same activity even if its
	// JSON re-serializes slightly differently byte-for-byte.
	ExternalID string `json:"external_id"`
	parse.JSONActivity
}

type syncActivitiesRequest struct {
	Source     string                `json:"source"`
	Activities []syncActivityRequest `json:"activities"`
}

// syncActivityResult reports what happened to one activity in the batch — a batch is never
// all-or-nothing, the same "one bad entry doesn't abort the rest" treatment handleZipUpload
// already gives a mixed-quality zip archive.
type syncActivityResult struct {
	ExternalID string `json:"external_id"`
	Status     string `json:"status"` // "enqueued" | "already_processed" | "rejected"
	Error      string `json:"error,omitempty"`
}

type syncActivitiesResponse struct {
	Results []syncActivityResult `json:"results"`
}

// handleSyncActivities serves IMPLEMENTATION.md §4.0's `POST /v1/sync/activities` — Path 2's
// batched normalized points. Like handleUpload (§4.1 step 1), this only validates, persists
// the raw payload, and enqueues an `ingest` job per activity; no parsing or privacy clipping
// happens inline. ingest.Process needs no Path-2-specific branch at all: each activity's raw
// payload is stored as JSON and read back through parse.ByExtension's ".json" case
// (parse.ParseJSON), the same "differ only in how bytes arrive, converge on §4.1 step 2"
// contract §4.0 states for all three paths.
//
// Idempotent on (user_id, source, external_id) per §4.0's hard invariant, enforced the same
// two-layer way as every other path: persistAndEnqueue's existence check here is the fast
// path, and ingest.Process's `ON CONFLICT DO NOTHING` at persist time is the actual guarantee
// under concurrent duplicates.
func (s *Server) handleSyncActivities(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSyncBodyBytes)

	var req syncActivitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !syncSources[req.Source] {
		http.Error(w, `invalid "source", want "healthconnect", "healthkit" or "recorded"`, http.StatusBadRequest)
		return
	}
	if len(req.Activities) == 0 {
		http.Error(w, "activities must not be empty", http.StatusBadRequest)
		return
	}
	if len(req.Activities) > maxSyncBatchActivities {
		http.Error(w, fmt.Sprintf("too many activities in one batch (%d), want %d or fewer", len(req.Activities), maxSyncBatchActivities), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	userID := userIDFromContext(ctx)
	results := make([]syncActivityResult, len(req.Activities))
	for i, act := range req.Activities {
		results[i] = s.syncOneActivity(ctx, userID, req.Source, act)
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, syncActivitiesResponse{Results: results})
}

// syncOneActivity validates and persists+enqueues a single batch entry, isolated into its own
// function so a marshal or persistAndEnqueue failure on one activity can't unwind the loop
// handling the rest of the batch.
func (s *Server) syncOneActivity(ctx context.Context, userID, source string, act syncActivityRequest) syncActivityResult {
	result := syncActivityResult{ExternalID: act.ExternalID}

	if act.ExternalID == "" {
		result.Status, result.Error = "rejected", "external_id is required"
		return result
	}
	// Same >= 2 raw points floor ingest.Process itself enforces on the raw points
	// (internal/ingest.Process) — a coarse pre-check here, not a claim that every activity
	// clearing it will also survive the trim; that deeper rejection still happens
	// asynchronously in the worker, exactly as it already does for Path 3 uploads.
	if len(act.Points) < 2 {
		result.Status, result.Error = "rejected", "an activity needs at least 2 points"
		return result
	}
	if len(act.Points) > maxSyncPointsPerActivity {
		result.Status, result.Error = "rejected", fmt.Sprintf("too many points (%d), want %d or fewer", len(act.Points), maxSyncPointsPerActivity)
		return result
	}
	// Same bounds handleUpdateActivity enforces for an edit after the fact — these columns
	// are VARCHAR(50)/VARCHAR(200)/TEXT-but-bounded regardless of which path sets them first.
	// activity_type specifically matters now that a client can send an arbitrary custom value
	// (GPS Logger's free-text type field) rather than one of a fixed vocabulary — unchecked,
	// an over-length value would fail as a raw, unhandled column-width error at insert time
	// deep in the worker instead of a clean rejection here.
	if len(act.ActivityType) > maxActivityTypeLen {
		result.Status, result.Error = "rejected", fmt.Sprintf("activity_type must be %d characters or fewer", maxActivityTypeLen)
		return result
	}
	if len(act.Name) > maxActivityNameLen {
		result.Status, result.Error = "rejected", fmt.Sprintf("name must be %d characters or fewer", maxActivityNameLen)
		return result
	}
	if len(act.Description) > maxActivityDescriptionLen {
		result.Status, result.Error = "rejected", fmt.Sprintf("description must be %d characters or fewer", maxActivityDescriptionLen)
		return result
	}

	// Re-marshal the embedded parse.JSONActivity, not the whole syncActivityRequest — the raw
	// payload persisted to object storage is exactly what parse.ParseJSON expects to decode
	// back (activity_type + points), with no external_id or other request-envelope fields
	// mixed in.
	data, err := json.Marshal(act.JSONActivity)
	if err != nil {
		s.log.Error("sync activity marshal failed", "external_id", act.ExternalID, "err", err)
		result.Status, result.Error = "rejected", "internal error"
		return result
	}

	externalID, alreadyProcessed, err := s.persistAndEnqueue(ctx, uploadFileParams{
		UserID:     userID,
		Source:     source,
		Filename:   act.ExternalID + ".json",
		Ext:        ".json",
		Data:       data,
		ExternalID: act.ExternalID,
	})
	if err != nil {
		s.log.Error("sync activity persist/enqueue failed", "external_id", act.ExternalID, "err", err)
		result.Status, result.Error = "rejected", "internal error"
		return result
	}

	result.ExternalID = externalID
	if alreadyProcessed {
		result.Status = "already_processed"
	} else {
		result.Status = "enqueued"
	}
	return result
}
