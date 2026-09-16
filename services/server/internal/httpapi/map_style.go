package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fitmap/fitmap/services/server/internal/mapstyle"
)

// handleMapStyle serves the MapLibre style document for one flavor — ARCHITECTURE.md §2.1's
// "serve the style as a document from the API", the prerequisite Android's map rendering
// blocks on (apps/android/docs/ROADMAP.md, Phase 2).
//
// The document's basemap asset URLs (glyphs, sprites, the .pmtiles archive) are resolved
// against this server's configured BASEMAP_ORIGIN at request time rather than baked in at
// build time, so one binary serves a deployment that bundles the archive alongside itself
// and one that reads it from a CDN — the same split apps/web's VITE_BASEMAP_ORIGIN already
// handles on its side.
//
// Note for a native client: the basemap source URL uses the `pmtiles://` scheme, which is a
// protocol MapLibre GL JS registers via the pmtiles package and MapLibre Native does not
// have. Deciding how Native reads the archive is its own open item on the Android roadmap;
// this endpoint serves the style as the web client defines it and does not pre-empt that.
func (s *Server) handleMapStyle(w http.ResponseWriter, r *http.Request) {
	flavor := strings.TrimSuffix(r.PathValue("flavor"), ".json")

	doc, err := mapstyle.Document(flavor, s.basemapOrigin)
	if errors.Is(err, mapstyle.ErrUnknownFlavor) {
		// Name the valid set rather than a bare 404 — the caller is a client developer
		// wiring this up, and the flavor list is not discoverable from anywhere else.
		http.Error(w, "unknown flavor; expected one of "+strings.Join(mapstyle.Flavors(), ", "), http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("map style", "err", err)
		http.Error(w, "failed to build style", http.StatusInternalServerError)
		return
	}

	etag := mapstyle.ETag(doc)
	w.Header().Set("ETag", etag)
	// Revalidate rather than cache blind: the document changes when style.ts is regenerated
	// or the origin is reconfigured, and a client holding a stale one points at asset URLs
	// that may no longer resolve. A 304 costs one round trip and no body.
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(doc); err != nil {
		s.log.Warn("map style write", "err", err)
	}
}
