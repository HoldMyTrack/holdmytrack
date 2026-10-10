package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// trackPointsResponse is what the track editor (§4.7.7) opens with: every point the activity
// was processed from, before the user's own edit, plus that edit — the client replays one on
// the other, so it can both show the current track and offer to reset it.
//
// Points are [lon, lat, t] with t in unix milliseconds, the unit TrackEdit addresses points by.
type trackPointsResponse struct {
	Points [][3]float64      `json:"points"`
	Edit   *ingest.TrackEdit `json:"edit"`
}

// handleActivityTrackPoints serves `GET /v1/activities/track-points/{id}`. The points come
// from the raw payload — the only place full-resolution positions survive (§4.1 step 6) — and
// are clipped against the account's current Private locations before they leave the server:
// the raw payload still holds the hidden ends, and returning them would hand the client the
// very places those locations exist to hide. A piece of a split (§4.7.8) gets its own range
// of them only.
func (s *Server) handleActivityTrackPoints(w http.ResponseWriter, r *http.Request) {
	activityID, ok := activityIDFromPath(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	var sourceDetail string
	var rawKey *string
	var editJSON []byte
	var rng ingest.SplitRange
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(source_detail, ''), raw_payload_key, track_edit, split_from, split_to
		FROM activities WHERE id = $1 AND user_id = $2
	`, activityID, userID).Scan(&sourceDetail, &rawKey, &editJSON, &rng.From, &rng.To)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "activity not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("track points lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if rawKey == nil {
		httpErrorT(w, r, http.StatusConflict, "error.track_no_points")
		return
	}

	points, _, err := ingest.LoadPiecePoints(ctx, s.pool, s.store, userID, sourceDetail, *rawKey, rng)
	if err != nil {
		s.log.Error("track points load failed", "activity_id", activityID, "err", err)
		httpErrorT(w, r, http.StatusConflict, "error.track_unreadable")
		return
	}
	if points == nil {
		httpErrorT(w, r, http.StatusConflict, "error.track_all_private")
		return
	}
	if err := ingest.EditableTimestamps(points); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	resp := trackPointsResponse{Points: make([][3]float64, len(points))}
	for i, p := range points {
		// 1e-6° is ~0.1 m — far finer than GPS, and it roughly halves the response size.
		resp.Points[i] = [3]float64{math.Round(p.Lon*1e6) / 1e6, math.Round(p.Lat*1e6) / 1e6, float64(p.Time.UnixMilli())}
	}
	if editJSON != nil {
		var e ingest.TrackEdit
		if err := json.Unmarshal(editJSON, &e); err != nil {
			s.log.Error("stored track edit unreadable", "activity_id", activityID, "err", err)
		} else {
			resp.Edit = &e
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// trackEditRequest is the complete new edit, not a delta. A null or empty edit resets the
// activity to its original track. SplitAt, a point timestamp in unix milliseconds, also splits
// the activity there (§4.7.8), the edit applying to both pieces. AllowHidden goes ahead with a
// split that leaves a piece entirely inside Private locations, once the user has been told.
type trackEditRequest struct {
	Edit        *ingest.TrackEdit `json:"edit"`
	SplitAt     *int64            `json:"split_at"`
	AllowHidden bool              `json:"allow_hidden"`
}

// splitHidesPartCode is the 422's `error` for a split that would leave a piece with no track,
// which the client asks about and resends with allow_hidden.
const splitHidesPartCode = "split_hides_part"

// maxTrackEditEntries bounds the spec's size — Delete point adds one timestamp per click and
// Move point one per point dragged, so this is far past any real editing session while still rejecting an absurd payload.
const maxTrackEditEntries = 10000

// handleActivityTrackEdit serves `POST /v1/activities/track-edit/{id}`: validate the spec,
// mark the activity pending, and enqueue an `edit_track` job to reprocess it (§4.7.7). The
// reprocessing itself — reparse, metrics, masks, fog/heatmap invalidation — is the same heavy
// work ingest does, so it goes through the job queue, never inline. Responds 202; the
// activity list reports `pending` until the job finishes.
func (s *Server) handleActivityTrackEdit(w http.ResponseWriter, r *http.Request) {
	activityID, ok := activityIDFromPath(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())

	var req trackEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Edit != nil {
		if err := req.Edit.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Edit.Remove)+len(req.Edit.Drop)+len(req.Edit.Move) > maxTrackEditEntries {
			httpErrorT(w, r, http.StatusBadRequest, "error.track_edit_too_large")
			return
		}
		if req.Edit.IsEmpty() {
			req.Edit = nil
		}
	}

	if req.SplitAt != nil {
		status, key := s.checkSplit(r, userID, activityID, req.Edit, *req.SplitAt)
		if key == "error.split_in_private" {
			if !req.AllowHidden {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error":   splitHidesPartCode,
					"message": i18n.Get(requestLang(r)).T(key),
				})
				return
			}
		} else if status != 0 {
			httpErrorT(w, r, status, key)
			return
		}
	}

	ok, err := ingest.EnqueueTrackEdit(r.Context(), s.pool, ingest.EditJob{UserID: userID, ActivityID: activityID, Edit: req.Edit}, req.SplitAt)
	if errors.Is(err, ingest.ErrSplitConflict) {
		httpErrorT(w, r, http.StatusConflict, "error.track_busy")
		return
	}
	if err != nil {
		s.log.Error("track edit enqueue failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		// Not this user's, or already mid-edit — one 409 covers both without saying which, same
		// as a 404 elsewhere never says whose activity it was.
		httpErrorT(w, r, http.StatusConflict, "error.track_busy")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// checkSplit is the split's one check that needs the points: both pieces must keep at least
// two of the points the edit leaves. A piece left entirely inside Private locations is reported
// separately (error.split_in_private): allowed, once the user knows it'll have no track. Done here rather than in the job so a split that can't
// work is refused before it creates a row. The range itself is checked again inside
// EnqueueTrackEdit's transaction. Returns 0 when the split is fine, else the status and message
// key to refuse it with.
func (s *Server) checkSplit(r *http.Request, userID, activityID string, edit *ingest.TrackEdit, at int64) (int, string) {
	ctx := r.Context()
	var sourceDetail string
	var rawKey *string
	var rng ingest.SplitRange
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(source_detail, ''), raw_payload_key, split_from, split_to
		FROM activities WHERE id = $1 AND user_id = $2
	`, activityID, userID).Scan(&sourceDetail, &rawKey, &rng.From, &rng.To)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rawKey == nil) {
		return http.StatusConflict, "error.track_busy"
	}
	if err != nil {
		s.log.Error("split lookup failed", "err", err)
		return http.StatusInternalServerError, "error.internal"
	}
	points, zones, err := ingest.LoadPiecePoints(ctx, s.pool, s.store, userID, sourceDetail, *rawKey, rng)
	if err != nil || points == nil {
		return http.StatusConflict, "error.track_unreadable"
	}
	switch ingest.CheckSplit(points, zones, edit, at) {
	case ingest.ErrSplitTooClose:
		return http.StatusBadRequest, "error.split_too_close"
	case ingest.ErrSplitHidden:
		return http.StatusBadRequest, "error.split_in_private"
	}
	return 0, ""
}

// trackMergeRequest names the pieces of one split to join back up.
type trackMergeRequest struct {
	IDs []string `json:"ids"`
}

// trackMergeResponse is the activity the pieces were joined into, and the ids that are gone.
type trackMergeResponse struct {
	ID      string   `json:"id"`
	Removed []string `json:"removed"`
}

// maxMergeIDs bounds the request: a split is a handful of pieces cut by hand.
const maxMergeIDs = 100

// handleActivityTrackMerge serves `POST /v1/activities/track-merge` (§4.7.8): join adjacent
// pieces of one split back into one activity. The joining itself is a row update and deletes
// (ingest.MergePieces); the rebuilt track comes from the same `edit_track` job an edit uses,
// so the survivor reads Pending until it lands. Responds 200 with the survivor and the
// deleted ids.
func (s *Server) handleActivityTrackMerge(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	var req trackMergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) > maxMergeIDs {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	for i, id := range req.IDs {
		if !uuidPattern.MatchString(id) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.IDs[i] = strings.ToLower(id)
	}

	survivor, removed, err := ingest.MergePieces(ctx, s.pool, userID, req.IDs)
	if errors.Is(err, ingest.ErrSplitConflict) {
		httpErrorT(w, r, http.StatusConflict, "error.merge_not_pieces")
		return
	}
	if err != nil {
		s.log.Error("track merge failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, trackMergeResponse{ID: survivor, Removed: removed})
}
