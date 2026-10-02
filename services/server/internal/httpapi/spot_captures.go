package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// spotCaptureSlackM is how far outside a spot's area a capture's position may still be (§4.25):
// the phone only counts a fix inside the area, but a GPS fix is a few metres off either way, and
// the server's check shouldn't refuse a capture the phone rightly showed as done.
const spotCaptureSlackM = 10

// spotDetailQuery is one place with its whole area and the caller's capture of it, if any —
// what the Android app's capture mode measures a position against (FR-15.6). A retired place
// (ADR-0027) is there only for an account that captured it.
const spotDetailQuery = `
SELECT s.id, s.category, s.name, s.address, s.description, s.inscription, s.memorial,
       s.start_date, s.wikipedia, ST_X(p.pt), ST_Y(p.pt), ST_AsGeoJSON(s.geom, 7), c.captured_at
FROM spots s
CROSS JOIN LATERAL (SELECT ST_PointOnSurface(s.geom) AS pt) p
LEFT JOIN spot_captures c ON c.spot_id = s.id AND c.user_id = $2
WHERE s.id = $1 AND (s.retired_at IS NULL OR c.user_id IS NOT NULL)`

// spotDetailJSON is `GET /v1/spots/{id}`: the place as "Show in this area" has it, its area as a
// GeoJSON MultiPolygon, and when the caller captured it (absent when they haven't).
type spotDetailJSON struct {
	spotJSON
	Area       json.RawMessage `json:"area"`
	CapturedAt *time.Time      `json:"captured_at,omitempty"`
}

type spotCaptureJSON struct {
	SpotID     int64     `json:"spot_id"`
	CapturedAt time.Time `json:"captured_at"`
}

type spotCapturesResponse struct {
	Captures []spotCaptureJSON `json:"captures"`
}

type spotCaptureRequest struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

func spotIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "spot not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// handleSpotDetail serves `GET /v1/spots/{id}` (FR-15.6).
func (s *Server) handleSpotDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := spotIDFromPath(w, r)
	if !ok {
		return
	}
	var sp spotDetailJSON
	var area string
	err := s.pool.QueryRow(r.Context(), spotDetailQuery, id, userIDFromContext(r.Context())).Scan(
		&sp.ID, &sp.Category, &sp.Name, &sp.Address, &sp.Description, &sp.Inscription, &sp.Memorial,
		&sp.StartDate, &sp.Wikipedia, &sp.Lon, &sp.Lat, &area, &sp.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "spot not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("spot detail query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sp.Area = json.RawMessage(area)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, sp)
}

// handleListSpotCaptures serves `GET /v1/spots/captures` (FR-15.6): every place the caller has
// captured, newest first — what the maps draw the captured badge from.
func (s *Server) handleListSpotCaptures(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT spot_id, captured_at FROM spot_captures WHERE user_id = $1 ORDER BY captured_at DESC, spot_id`,
		userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error("spot captures query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := spotCapturesResponse{Captures: []spotCaptureJSON{}}
	captures, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (spotCaptureJSON, error) {
		var c spotCaptureJSON
		err := row.Scan(&c.SpotID, &c.CapturedAt)
		return c, err
	})
	if err != nil {
		s.log.Error("spot captures scan failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp.Captures = append(resp.Captures, captures...)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// handleCaptureSpot serves `POST /v1/spots/{id}/captures` (FR-15.6): the phone has held the caller
// inside the place for 30 s, and sends the last position it measured. The server checks that
// position against the area once more, within spotCaptureSlackM, and keeps the first capture —
// 201 for a new one, 200 with the earlier one's time when the place was already captured. A
// retired place (ADR-0027) can't be captured: 410.
func (s *Server) handleCaptureSpot(w http.ResponseWriter, r *http.Request) {
	id, ok := spotIDFromPath(w, r)
	if !ok {
		return
	}
	var req spotCaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Lat == nil || req.Lon == nil ||
		math.Abs(*req.Lat) > 90 || math.Abs(*req.Lon) > 180 {
		http.Error(w, "lat and lon must be a position in degrees", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var inside, retired bool
	err := s.pool.QueryRow(ctx,
		`SELECT ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, $4), retired_at IS NOT NULL
		 FROM spots WHERE id = $1`,
		id, *req.Lon, *req.Lat, spotCaptureSlackM).Scan(&inside, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "spot not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("spot capture check failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if retired {
		http.Error(w, "the spot is no longer on the map", http.StatusGone)
		return
	}
	if !inside {
		http.Error(w, "the position is outside the spot", http.StatusUnprocessableEntity)
		return
	}
	userID := userIDFromContext(ctx)
	capture := spotCaptureJSON{SpotID: id}
	status := http.StatusCreated
	err = s.pool.QueryRow(ctx,
		`INSERT INTO spot_captures (user_id, spot_id) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING captured_at`,
		userID, id).Scan(&capture.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusOK
		err = s.pool.QueryRow(ctx, `SELECT captured_at FROM spot_captures WHERE user_id = $1 AND spot_id = $2`,
			userID, id).Scan(&capture.CapturedAt)
	}
	if err != nil {
		s.log.Error("spot capture failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, status, capture)
}
