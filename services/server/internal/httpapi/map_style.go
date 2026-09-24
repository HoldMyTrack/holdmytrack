package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mapstyle"
)

// handleMapStyle serves the MapLibre style document for one flavor — ARCHITECTURE.md §2.1's
// "serve the style as a document from the API", the prerequisite Android's map rendering
// blocks on (apps/android/docs/ARCHITECTURE.md §2.1).
//
// The document's basemap asset URLs (glyphs, sprites, the .pmtiles archive) are resolved
// against this server's configured BASEMAP_ORIGIN at request time rather than baked in at
// build time, so one binary serves a deployment that bundles the archive alongside itself
// and one that reads it from a CDN — the same split apps/web's VITE_BASEMAP_ORIGIN already
// handles on its side.
//
// The basemap source URL uses the `pmtiles://` scheme, which MapLibre GL JS answers with the
// pmtiles package's registered protocol and MapLibre Native answers natively (built in since
// Android 11.8.0 / iOS 6.10.0). So one document serves both: no native client needs the
// archive re-exposed as `{z}/{x}/{y}`, confirmed on a physical Android device reading this
// endpoint's own output (apps/android/docs/ARCHITECTURE.md §2.1).
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
