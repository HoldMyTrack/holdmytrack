package httpapi

import (
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fitmap/fitmap/services/server/internal/fog"
)

// handleHeatmapTile serves §4.2.2's additive-intensity mask as a ready-to-draw RGBA PNG via
// heatmapRamp — reading heatmap_object_key instead of object_key from the unfiltered fog_tiles
// row that Fog uses (handleFogTile), and fog.HeatmapKind instead of fog.FogKind for the
// composite.
//
// Unlike Fog of War, which shows true all-time coverage (a place once cleared stays cleared),
// Heatmap answers "where do I go *now*" — an old, no-longer-visited route should be able to
// cool off rather than stay maximally hot forever. So this always takes the on-the-fly
// filtered path (fog.RenderFilteredTile), with a fixed rolling window (fog.HeatmapWindowDays,
// computed here, not from the request) as the only filter — never a client-supplied date range
// or exclude list; a query string carrying either is ignored entirely. Unlike an arbitrary
// user-chosen range, a relative rolling window is inherently self-bounding — "activities in
// the last N days" costs roughly the same whether the account is one year old or ten — which
// is what makes the filtered path affordable to always take here, unlike the arbitrary ranges
// this same endpoint used to accept.
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
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	windowStart := time.Now().AddDate(0, 0, -fog.HeatmapWindowDays)
	filter := fog.Filter{From: &windowStart}

	mask, err := fog.RenderFilteredTile(ctx, s.pool, s.store, userID, fog.HeatmapKind, z, x, y, filter)
	if err != nil {
		s.log.Error("heatmap tile filtered render failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rgba := fog.RenderHeatmapPNG(mask)
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, rgba); err != nil {
		s.log.Error("heatmap tile encode failed", "err", err)
	}
}
