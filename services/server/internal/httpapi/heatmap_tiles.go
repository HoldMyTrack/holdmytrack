package httpapi

import (
	"errors"
	"image/png"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/fitmap/fitmap/services/server/internal/fog"
)

// handleHeatmapTile serves §4.2.2's additive-intensity mask as a ready-to-draw RGBA PNG.
//
// Unlike Fog of War, which shows true all-time coverage (a place once cleared stays cleared),
// Heatmap answers "where do I go *now*" — an old, no-longer-visited route should be able to
// cool off rather than stay maximally hot forever, so heatmap_object_key (fog_tiles) is built
// from only the activities inside a rolling window (fog.HeatmapWindowDays), not the account's
// whole history.
//
// This used to mean composing that window live, on every request (fog.RenderFilteredTile) —
// measured against the Demo Customer's home tile (591 of 611 activities), that took ~12.6s
// per tile and ~56.5s at low zoom, and the 45-second cache meant to absorb repeat requests
// within one pan/zoom gesture never actually hit: its key embedded the filter's exact `From`
// instant, which a fresh `time.Now().AddDate(0, 0, -N)` call almost never reproduces between
// two real requests, so every single tile request paid full compositing cost. Heatmap now
// reads a precomputed aggregate instead, exactly like Fog — internal/fog.renderAndStoreTile
// builds heatmap_object_key from only the window's masks, kept current the same way Fog's own
// aggregate is (ingest/delete dirty-marking) plus a weekly sweep (internal/worker's
// refreshHeatmapWindows) so the window's trailing edge keeps moving even without new uploads.
func (s *Server) handleHeatmapTile(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".png"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	var objectKey *string
	err := s.pool.QueryRow(ctx,
		`SELECT heatmap_object_key FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, z, x, y,
	).Scan(&objectKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Error("heatmap tile query failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mask, err := loadMaskOrBlank(ctx, s.store, objectKey)
	if err != nil {
		s.log.Error("heatmap tile fetch failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rgba := fog.RenderHeatmapPNG(mask)
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, rgba); err != nil {
		s.log.Error("heatmap tile encode failed", "err", err)
	}
}
