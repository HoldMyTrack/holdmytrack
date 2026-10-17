package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/HoldMyTrack/platform/services/server/internal/storage"
)

// loadMaskPNG fetches a stored fog_tiles tile's PNG as it is, or nil when there is nothing
// stored yet — "no coverage rendered here" is a normal state (a tile the user's history has
// never touched, or one still waiting on render_fog), not an error condition: fog.DecodeCoverage
// and fog.DecodeCounts take nil as a blank tile. Shared by handleFogVectorTile (object_key) and
// handleHeatPointsTile (heatmap_object_key).
func loadMaskPNG(ctx context.Context, store *storage.Store, objectKey *string) ([]byte, error) {
	if objectKey == nil {
		return nil, nil
	}
	obj, err := store.Get(ctx, *objectKey)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// statusClientClosedRequest is nginx's 499: the client went away before the answer was ready.
const statusClientClosedRequest = 499

// clientGone reports whether r's client has already disconnected, and if so answers 499. A
// map drops in-flight tile requests whenever it's zoomed or panned past them, so a cancelled
// query or object fetch there is routine, not a server fault: it isn't logged, and the 499
// keeps it out of the request metrics' 5xx count.
func clientGone(w http.ResponseWriter, r *http.Request) bool {
	if r.Context().Err() == nil {
		return false
	}
	w.WriteHeader(statusClientClosedRequest)
	return true
}
