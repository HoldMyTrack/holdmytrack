package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// syncSources is the `source` allowlist this endpoint accepts — IMPLEMENTATION.md §4.0 names
// the two on-device platforms for Path 2 (iOS/HealthKit, Android/Health Connect), and §4.0.4
// adds "recorded" for in-app GPS recording (ADR-0007) — authored by HoldMyTrack itself rather than
// read from a platform health store, but the same batched wire shape either way. §4.0.5 adds
// "timeline": a Google Maps Timeline export, read in the browser, whose movement segments
// arrive here already split into activities. Path 1 (webhooks) and Path 3's file upload have
// their own endpoints and their own `source` values, so this list doesn't need to anticipate
// those.
var syncSources = map[string]bool{"healthconnect": true, "healthkit": true, "recorded": true, "timeline": true}

// syncExternalIDPattern is what a synced activity's external_id may be: a Health Connect or
// HealthKit record UUID, or the UUID GPS-Logger mints, fits easily. The id goes into the raw
// payload's object key and into two VARCHAR(255) columns (source_detail as id + ".json"), so
// a "/" or ".." would nest or escape the key, and an over-long id was accepted as "enqueued"
// only for the worker's insert to fail on it.
var syncExternalIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,199}$`)

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
	// Batch and BatchTitle are optional: an import the client splits over several requests (a
	// Timeline export of thousands of activities, §4.0.5) names one batch for all of them, so
	// the Upload menu shows it as one row, "Timeline.json · 120 of 584", as it does a .zip.
	// Without one, each request is its own batch.
	Batch      string `json:"batch"`
	BatchTitle string `json:"batch_title"`
}

// syncBatchPattern is what a client-chosen batch id may be. It's stored with clientBatchPrefix,
// whose ":" neither newBatchID's tokens nor a lone upload's job id (activeImportsQuery groups
// those by id::text) can contain, so a client can't fold its activities into another row.
var syncBatchPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

const clientBatchPrefix = "client:"

// maxSyncBatchTitleRunes bounds a batch's title, a file name in practice.
const maxSyncBatchTitleRunes = 200

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
// batched normalized points. Like handleUpload (§4.1 step 1), this only validates and
// enqueues an `ingest` job per activity with its raw payload inline (ingest.EnqueueRaw), two
// queries however large the batch; no parsing or privacy clipping happens inline.
// ingest.Process needs no Path-2-specific branch at all: each activity's raw payload is
// stored as JSON and read back through parse.ByExtension's ".json" case
// (parse.ParseJSON), the same "differ only in how bytes arrive, converge on §4.1 step 2"
// contract §4.0 states for all three paths.
//
// Idempotent on (user_id, source, external_id) per §4.0's hard invariant, enforced the same
// two-layer way as every other path: EnqueueRaw's existence check here is the fast
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
		http.Error(w, `invalid "source", want "healthconnect", "healthkit", "recorded" or "timeline"`, http.StatusBadRequest)
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

	batch := newBatchID()
	if req.Batch != "" {
		if !syncBatchPattern.MatchString(req.Batch) {
			http.Error(w, `invalid "batch", want 8 to 64 letters, digits, "-" or "_"`, http.StatusBadRequest)
			return
		}
		batch = clientBatchPrefix + req.Batch
	}
	if utf8.RuneCountInString(req.BatchTitle) > maxSyncBatchTitleRunes {
		http.Error(w, fmt.Sprintf(`"batch_title" too long, want %d characters or fewer`, maxSyncBatchTitleRunes), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	userID := userIDFromContext(ctx)
	l := i18n.Get(requestLang(r))
	results := make([]syncActivityResult, len(req.Activities))
	var items []ingest.RawItem
	var slots []int // results index of each item
	for i, act := range req.Activities {
		item, rejection := s.syncItem(l, req.Source, batch, req.BatchTitle, act)
		if rejection != "" {
			results[i] = syncActivityResult{ExternalID: act.ExternalID, Status: "rejected", Error: rejection}
			continue
		}
		items = append(items, item)
		slots = append(slots, i)
	}
	enqueued, err := ingest.EnqueueRaw(ctx, s.pool, userID, items)
	if err != nil {
		s.log.Error("sync enqueue failed", "err", err, "activities", len(items))
	}
	for k, i := range slots {
		switch {
		case err != nil:
			results[i] = syncActivityResult{ExternalID: items[k].ExternalID, Status: "rejected", Error: l.T("error.internal")}
		case enqueued[k].AlreadyProcessed:
			results[i] = syncActivityResult{ExternalID: enqueued[k].ExternalID, Status: "already_processed"}
		default:
			results[i] = syncActivityResult{ExternalID: enqueued[k].ExternalID, Status: "enqueued"}
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, syncActivitiesResponse{Results: results})
}

// syncItem validates one batch entry and turns it into the item EnqueueRaw takes, or returns
// why it was rejected — one bad entry is that entry's result, never the whole batch's.
func (s *Server) syncItem(l *i18n.Localizer, source, batch, batchTitle string, act syncActivityRequest) (ingest.RawItem, string) {
	if act.ExternalID == "" {
		return ingest.RawItem{}, l.T("error.sync_external_id_required")
	}
	if !syncExternalIDPattern.MatchString(act.ExternalID) {
		return ingest.RawItem{}, l.T("error.sync_external_id_invalid")
	}
	// Same >= 2 raw points floor ingest.Process itself enforces on the raw points
	// (internal/ingest.Process) — a coarse pre-check here, not a claim that every activity
	// clearing it will also survive the trim; that deeper rejection still happens
	// asynchronously in the worker, exactly as it already does for Path 3 uploads.
	if len(act.Points) < 2 {
		return ingest.RawItem{}, l.T("error.sync_too_few_points")
	}
	if len(act.Points) > maxSyncPointsPerActivity {
		return ingest.RawItem{}, l.T("error.sync_too_many_points", "n", len(act.Points), "max", maxSyncPointsPerActivity)
	}
	// Same bounds handleUpdateActivity enforces for an edit after the fact — these columns
	// are VARCHAR(50)/VARCHAR(200)/TEXT-but-bounded regardless of which path sets them first.
	// activity_type specifically matters now that a client can send an arbitrary custom value
	// (GPS Logger's free-text type field) rather than one of a fixed vocabulary — unchecked,
	// an over-length value would fail as a raw, unhandled column-width error at insert time
	// deep in the worker instead of a clean rejection here.
	if len(act.ActivityType) > maxActivityTypeLen {
		return ingest.RawItem{}, l.T("error.activity_type_too_long", "max", maxActivityTypeLen)
	}
	if len(act.Name) > maxActivityNameLen {
		return ingest.RawItem{}, l.T("error.activity_name_too_long", "max", maxActivityNameLen)
	}
	if len(act.Description) > maxActivityDescriptionLen {
		return ingest.RawItem{}, l.T("error.activity_description_too_long", "max", maxActivityDescriptionLen)
	}

	// Re-marshal the embedded parse.JSONActivity, not the whole syncActivityRequest — the raw
	// payload stored for the job is exactly what parse.ParseJSON expects to decode
	// back (activity_type + points), with no external_id or other request-envelope fields
	// mixed in.
	data, err := json.Marshal(act.JSONActivity)
	if err != nil {
		s.log.Error("sync activity marshal failed", "external_id", act.ExternalID, "err", err)
		return ingest.RawItem{}, l.T("error.internal")
	}
	return ingest.RawItem{
		Source:     source,
		Filename:   act.ExternalID + ".json",
		Ext:        ".json",
		Data:       data,
		ExternalID: act.ExternalID,
		Batch:      batch,
		BatchTitle: batchTitle,
	}, ""
}
