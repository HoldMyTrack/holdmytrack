package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// Private locations (FR-8.1, IMPLEMENTATION.md §7) — circles on the map whose contents never
// leave ingest. Stored in privacy_zones (§3.7). Every change enqueues a `reprivacy` job over
// the activities the old and new circle could clip, marking them Pending in the same
// transaction, so the Activity List shows which rows are being reprocessed.

const (
	minPrivateLocationRadiusM = 50
	maxPrivateLocationRadiusM = 2000
	maxPrivateLocations       = 20
	maxPrivateLocationNameLen = 100
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type privateLocation struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Lon     float64 `json:"lon"`
	Lat     float64 `json:"lat"`
	RadiusM int     `json:"radius_m"`
}

// privateLocationRequest is a full replace for PATCH as for POST — the map editor always
// sends the circle's whole current state, the same one-form shape handleUpdateSettings uses.
type privateLocationRequest struct {
	Name    string  `json:"name"`
	Lon     float64 `json:"lon"`
	Lat     float64 `json:"lat"`
	RadiusM int     `json:"radius_m"`
}

func (req *privateLocationRequest) validate() error {
	req.Name = strings.TrimSpace(req.Name)
	if len([]rune(req.Name)) > maxPrivateLocationNameLen {
		return accountFailure(http.StatusBadRequest, "error.location_name_too_long", "max", maxPrivateLocationNameLen)
	}
	if math.IsNaN(req.Lat) || math.IsNaN(req.Lon) || req.Lat < -85 || req.Lat > 85 || req.Lon < -180 || req.Lon > 180 {
		return accountFailure(http.StatusBadRequest, "error.location_position")
	}
	if req.RadiusM < minPrivateLocationRadiusM || req.RadiusM > maxPrivateLocationRadiusM {
		return accountFailure(http.StatusBadRequest, "error.location_radius", "min", minPrivateLocationRadiusM, "max", maxPrivateLocationRadiusM)
	}
	return nil
}

func decodePrivateLocation(w http.ResponseWriter, r *http.Request) (privateLocationRequest, bool) {
	var req privateLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return req, false
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.(*accountError).message(requestLang(r)), http.StatusBadRequest)
		return req, false
	}
	return req, true
}

const selectPrivateLocation = `
	SELECT id, COALESCE(name, ''), ST_X(center::geometry), ST_Y(center::geometry), radius_m
	FROM privacy_zones`

func scanPrivateLocation(row pgx.Row) (privateLocation, error) {
	var l privateLocation
	err := row.Scan(&l.ID, &l.Name, &l.Lon, &l.Lat, &l.RadiusM)
	return l, err
}

// handleListPrivateLocations serves `GET /v1/private-locations`, oldest first.
func (s *Server) handleListPrivateLocations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), selectPrivateLocation+` WHERE user_id = $1 ORDER BY created_at, id`,
		userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error("private locations query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	locations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (privateLocation, error) {
		return scanPrivateLocation(row)
	})
	if err != nil {
		s.log.Error("private locations scan failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if locations == nil {
		locations = []privateLocation{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, locations)
}

// handleCreatePrivateLocation serves `POST /v1/private-locations`, responding 201 with the
// new location.
func (s *Server) handleCreatePrivateLocation(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePrivateLocation(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())

	var created privateLocation
	err := s.changePrivateLocations(r.Context(), userID, func(tx pgx.Tx) ([]circle, error) {
		var count int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM privacy_zones WHERE user_id = $1`, userID).Scan(&count); err != nil {
			return nil, err
		}
		if count >= maxPrivateLocations {
			return nil, errTooManyLocations
		}
		var err error
		created, err = scanPrivateLocation(tx.QueryRow(r.Context(), `
			INSERT INTO privacy_zones (user_id, name, center, radius_m)
			VALUES ($1, NULLIF($2, ''), ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5)
			RETURNING id, COALESCE(name, ''), ST_X(center::geometry), ST_Y(center::geometry), radius_m
		`, userID, req.Name, req.Lon, req.Lat, req.RadiusM))
		return []circle{{req.Lat, req.Lon, req.RadiusM}}, err
	})
	if s.writeLocationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdatePrivateLocation serves `PATCH /v1/private-locations/{id}` — move, resize or
// rename. Both the old and the new circle's activities are reprocessed: the old ones may get
// their hidden ends back, the new ones lose theirs.
func (s *Server) handleUpdatePrivateLocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		http.Error(w, "private location not found", http.StatusNotFound)
		return
	}
	req, ok := decodePrivateLocation(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())

	var updated privateLocation
	err := s.changePrivateLocations(r.Context(), userID, func(tx pgx.Tx) ([]circle, error) {
		old, err := scanPrivateLocation(tx.QueryRow(r.Context(),
			selectPrivateLocation+` WHERE id = $1 AND user_id = $2 FOR UPDATE`, id, userID))
		if err != nil {
			return nil, err
		}
		updated, err = scanPrivateLocation(tx.QueryRow(r.Context(), `
			UPDATE privacy_zones
			SET name = NULLIF($3, ''), center = ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography, radius_m = $6
			WHERE id = $1 AND user_id = $2
			RETURNING id, COALESCE(name, ''), ST_X(center::geometry), ST_Y(center::geometry), radius_m
		`, id, userID, req.Name, req.Lon, req.Lat, req.RadiusM))
		if err != nil {
			return nil, err
		}
		if old.Lat == updated.Lat && old.Lon == updated.Lon && old.RadiusM == updated.RadiusM {
			return nil, nil // renamed only: nothing on the map changes
		}
		return []circle{{old.Lat, old.Lon, old.RadiusM}, {updated.Lat, updated.Lon, updated.RadiusM}}, nil
	})
	if s.writeLocationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeletePrivateLocation serves `DELETE /v1/private-locations/{id}`, responding 204.
func (s *Server) handleDeletePrivateLocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		http.Error(w, "private location not found", http.StatusNotFound)
		return
	}
	userID := userIDFromContext(r.Context())

	err := s.changePrivateLocations(r.Context(), userID, func(tx pgx.Tx) ([]circle, error) {
		old, err := scanPrivateLocation(tx.QueryRow(r.Context(), `
			DELETE FROM privacy_zones WHERE id = $1 AND user_id = $2
			RETURNING id, COALESCE(name, ''), ST_X(center::geometry), ST_Y(center::geometry), radius_m
		`, id, userID))
		return []circle{{old.Lat, old.Lon, old.RadiusM}}, err
	})
	if s.writeLocationError(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type circle struct {
	lat, lon float64
	radiusM  int
}

var errTooManyLocations = accountFailure(http.StatusConflict, "error.location_too_many", "max", maxPrivateLocations)

// changePrivateLocations runs one privacy_zones change and enqueues the reprocessing it
// causes in a single transaction: change returns the circles (old and/or new) whose
// activities need reprocessing.
func (s *Server) changePrivateLocations(ctx context.Context, userID string, change func(pgx.Tx) ([]circle, error)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	circles, err := change(tx)
	if err != nil {
		return err
	}
	if len(circles) > 0 {
		seen := map[string]bool{}
		var ids []string
		add := func(found []string) {
			for _, id := range found {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		for _, c := range circles {
			found, err := ingest.AffectedActivities(ctx, tx, userID, c.lat, c.lon, c.radiusM)
			if err != nil {
				return err
			}
			add(found)
		}
		hidden, err := ingest.AffectedHiddenActivities(ctx, tx, userID)
		if err != nil {
			return err
		}
		add(hidden)
		if err := ingest.EnqueueReprivacy(ctx, tx, ingest.ReprivacyJob{UserID: userID, ActivityIDs: ids}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// writeLocationError maps changePrivateLocations' error to a response, reporting whether
// it wrote one.
func (s *Server) writeLocationError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		http.Error(w, "private location not found", http.StatusNotFound)
	case errors.Is(err, errTooManyLocations):
		http.Error(w, errTooManyLocations.(*accountError).message(requestLang(r)), http.StatusConflict)
	default:
		s.log.Error("private location change failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	return true
}
