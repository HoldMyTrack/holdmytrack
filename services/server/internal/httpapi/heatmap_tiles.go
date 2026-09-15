package httpapi

import (
	"errors"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/fitmap/fitmap/services/server/internal/fog"
)

// handleHeatmapTile serves §4.2.2's additive-intensity mask as a ready-to-draw RGBA PNG via
// heatmapRamp — the same shape as handleFogTile (see its own doc comment for the unfiltered/
// filtered split), reading heatmap_object_key instead of object_key from the unfiltered
// fog_tiles row, and fog.HeatmapKind instead of fog.FogKind for a filtered composite.
//
// Unlike fog's blank tile (fully opaque white veil — "no coverage" must still read as
// fogged), a blank heatmap tile renders fully transparent: "no heat here" is genuinely
// nothing to draw, not a state that needs representing on screen.
func (s *Server) handleHeatmapTile(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".png"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}
	filter, err := parseMaskFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	var mask *image.Gray
	if filter.IsUnfiltered() {
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
		mask, err = loadMaskOrBlank(ctx, s.store, objectKey)
		if err != nil {
			s.log.Error("heatmap tile fetch failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		mask, err = fog.RenderFilteredTile(ctx, s.pool, s.store, userID, fog.HeatmapKind, z, x, y, filter)
		if err != nil {
			s.log.Error("heatmap tile filtered render failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	rgba := fog.RenderHeatmapPNG(mask)
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, rgba); err != nil {
		s.log.Error("heatmap tile encode failed", "err", err)
	}
}
