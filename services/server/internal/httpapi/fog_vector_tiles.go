package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/platform/services/server/internal/fog"
)

// handleFogVectorTile serves Fog as vector shapes (IMPLEMENTATION.md §4.2): the veil and its
// edges, traced from the same stored coverage tile handleFogTile colours — read, decoded and
// traced per request, so the render and what it stores don't change. The clients draw the veil
// as a fill and its edges as a blurred line, crisp at every zoom.
func (s *Server) handleFogVectorTile(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := tileCoords(w, r, ".mvt")
	if !ok {
		return
	}
	ctx := r.Context()
	var objectKey *string
	err := s.pool.QueryRow(ctx,
		`SELECT object_key FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userIDFromContext(ctx), z, x, y,
	).Scan(&objectKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.fogVectorFailed(w, r, "row", err, z, x, y)
		return
	}
	stored, err := loadMaskPNG(ctx, s.store, objectKey)
	if err != nil {
		s.fogVectorFailed(w, r, "fetch", err, z, x, y)
		return
	}
	cov, err := fog.DecodeCoverage(stored)
	if err != nil {
		s.fogVectorFailed(w, r, "decode", err, z, x, y)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(fog.EncodeFogTile(cov, z < fog.Zoom)) // fails only when the client has gone
}

func (s *Server) fogVectorFailed(w http.ResponseWriter, r *http.Request, step string, err error, z, x, y int) {
	if clientGone(w, r) {
		return
	}
	s.log.Error("fog vector tile failed", "step", step, "err", err, "z", z, "x", x, "y", y)
	writeAPIError(w, "internal error", http.StatusInternalServerError)
}
