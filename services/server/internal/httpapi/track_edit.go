package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/jackc/pgx/v5"

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
// are privacy-trimmed with the account's current setting before they leave the server: the
// raw payload still holds the trimmed-off ends, and returning them would hand the client the
// very home location the trim exists to hide.
func (s *Server) handleActivityTrackPoints(w http.ResponseWriter, r *http.Request) {
	activityID := r.PathValue("id")
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	var sourceDetail string
	var rawKey *string
	var editJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(source_detail, ''), raw_payload_key, track_edit
		FROM activities WHERE id = $1 AND user_id = $2
	`, activityID, userID).Scan(&sourceDetail, &rawKey, &editJSON)
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
		http.Error(w, "this activity has no recorded points to edit", http.StatusConflict)
		return
	}

	points, err := ingest.LoadTrimmedPoints(ctx, s.store, sourceDetail, *rawKey, float64(privacyTrimCmFromContext(ctx))/100)
	if err != nil {
		s.log.Error("track points load failed", "activity_id", activityID, "err", err)
		http.Error(w, "couldn't read this activity's recorded points", http.StatusConflict)
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
// activity to its original track.
type trackEditRequest struct {
	Edit *ingest.TrackEdit `json:"edit"`
}

// maxTrackEditEntries bounds the spec's size — Delete point adds one timestamp per click, so
// this is far past any real editing session while still rejecting an absurd payload.
const maxTrackEditEntries = 10000

// handleActivityTrackEdit serves `POST /v1/activities/track-edit/{id}`: validate the spec,
// mark the activity pending, and enqueue an `edit_track` job to reprocess it (§4.7.7). The
// reprocessing itself — reparse, metrics, masks, fog/heatmap invalidation — is the same heavy
// work ingest does, so it goes through the job queue, never inline. Responds 202; the
// activity list reports `pending` until the job finishes.
func (s *Server) handleActivityTrackEdit(w http.ResponseWriter, r *http.Request) {
	activityID := r.PathValue("id")
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
		if len(req.Edit.Remove)+len(req.Edit.Drop) > maxTrackEditEntries {
			http.Error(w, "edit is too large", http.StatusBadRequest)
			return
		}
		if req.Edit.IsEmpty() {
			req.Edit = nil
		}
	}

	ok, err := ingest.EnqueueTrackEdit(r.Context(), s.pool, ingest.EditJob{UserID: userID, ActivityID: activityID, Edit: req.Edit})
	if err != nil {
		s.log.Error("track edit enqueue failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		// Not this user's, a superseded duplicate, or already mid-edit — one 409 covers all
		// three without saying which, same as a 404 elsewhere never says whose activity it was.
		http.Error(w, "this activity can't be edited right now", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
