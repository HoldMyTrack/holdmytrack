package httpapi

import (
	"errors"
	"image/png"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
)

// handleFogTile serves §4.2's coverage mask as a ready-to-draw fog-veil RGBA PNG: the veil's
// colour in RGB, alpha = veil opacity × (255 - coverage).
//
// Always reads the precomputed fog_tiles aggregate — Fog of War shows true all-time coverage,
// unconditionally: it isn't scoped by date range, TYPE/DISTANCE, or hidden-track state (a
// place once cleared stays cleared, which is the whole point of the mechanic), so there is no
// per-request filter to honor and no on-the-fly compositing here. handleHeatmapTile is the
// same shape now too — see its own doc comment for why it's a plain lookup as well, not a
// live composite, despite Heatmap's rolling window.
//
// `?theme=dark` picks the cream veil drawn over dark basemaps; anything else gets the dark ink
// veil (fog.VeilForTheme). The theme is part of the URL, so a cached tile is one theme's.
func (s *Server) handleFogTile(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := tileCoords(w, r, ".png")
	if !ok {
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	var objectKey *string
	err := s.pool.QueryRow(ctx,
		`SELECT object_key FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, z, x, y,
	).Scan(&objectKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if clientGone(w, r) {
			return
		}
		s.log.Error("fog tile query failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mask, err := loadMaskOrBlank(ctx, s.store, objectKey)
	if err != nil {
		if clientGone(w, r) {
			return
		}
		s.log.Error("fog tile fetch failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rgba := fog.RenderFogPNG(mask, fog.VeilForTheme(r.URL.Query().Get("theme")))
	w.Header().Set("Content-Type", "image/png")
	setTileCacheControl(w, r)
	// Encoding an in-memory image fails only on the write, which is the client having gone.
	_ = png.Encode(w, rgba)
}
