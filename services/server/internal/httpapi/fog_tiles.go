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

// handleFogTile serves §4.2's coverage mask as a ready-to-draw white-veil RGBA PNG:
// fog_colour in RGB, alpha = fog_opacity × (255 - coverage).
//
// Unfiltered (the common case — no date narrowing, nothing hidden): reads the precomputed
// fog_tiles aggregate, exactly as before this endpoint gained filter support — no new work
// in this path. Filtered (`from`/`to`/`exclude` present): composites on the fly from
// per-activity masks instead (fog.RenderFilteredTile) — see IMPLEMENTATION.md
// on why fog/heatmap couldn't answer filters the way tracks tiles already do, and why the
// per-activity-mask redesign is what makes this affordable without re-parsing raw GPS files
// per request.
//
// `theme` is accepted, per §4.2's own reasoning for putting it in the URL (so a CDN caches
// one variant per theme) — but there is only one documented veil treatment (§4.2.1's white
// veil), so it's applied regardless of the theme value for now. A dark-theme variant is a
// real, undecided gap, not silently invented here.
func (s *Server) handleFogTile(w http.ResponseWriter, r *http.Request) {
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
			`SELECT object_key FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
			userID, z, x, y,
		).Scan(&objectKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("fog tile query failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		mask, err = loadMaskOrBlank(ctx, s.store, objectKey)
		if err != nil {
			s.log.Error("fog tile fetch failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		mask, err = fog.RenderFilteredTile(ctx, s.pool, s.store, userID, fog.FogKind, z, x, y, filter)
		if err != nil {
			s.log.Error("fog tile filtered render failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	rgba := fog.RenderFogPNG(mask)
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, rgba); err != nil {
		s.log.Error("fog tile encode failed", "err", err)
	}
}
