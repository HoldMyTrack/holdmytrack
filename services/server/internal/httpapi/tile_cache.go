package httpapi

import (
	"net/http"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
)

// setTileCacheControl is every per-user tile route's caching (IMPLEMENTATION.md §4.2's
// "Caching"): a request carrying the caller's own tile version as `cv` may be kept for good,
// since any change to what it returns bumps map_version (fog.BumpMapVersion) and with it the
// URL a client asks for next. `private` because the tile is the caller's own coverage, which
// points at where they live — only their browser or app may keep it, never a shared cache.
// Without a `cv` (an older client, or one that hasn't read a version yet), or with another
// account's, nothing may be reused without asking again.
func setTileCacheControl(w http.ResponseWriter, r *http.Request) {
	if fog.TileVersionOwnedBy(r.URL.Query().Get("cv"), userIDFromContext(r.Context())) {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-cache")
}
