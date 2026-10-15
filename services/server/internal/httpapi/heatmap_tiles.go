package httpapi

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"sync"
)

// handleHeatmapTile answers the Heatmap raster tiles Android builds from before the heat points
// (IMPLEMENTATION.md §4.2.2) still ask for, with a transparent tile: those builds see no heat.
// The rasters they drew are gone — Heatmap's stored tiles are pass counts now — and serving
// stale ones would keep showing an activity deleted, or a place put inside a Private location,
// since. docs/ROADMAP.md has removing the route once those builds are gone.
func (s *Server) handleHeatmapTile(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := tileCoords(w, r, ".png"); !ok {
		return
	}
	w.Header().Set("Content-Type", "image/png")
	setTileCacheControl(w, r)
	_, _ = w.Write(transparentPNG())
}

// transparentPNG is a 1×1 fully transparent PNG, encoded once.
var transparentPNG = sync.OnceValue(func() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err) // encoding an in-memory image to a bytes.Buffer can't fail
	}
	return buf.Bytes()
})
