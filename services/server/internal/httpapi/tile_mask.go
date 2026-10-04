package httpapi

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// loadMaskOrBlank fetches and decodes a stored fog_tiles mask, or returns a blank (all-zero)
// one when there is nothing stored yet — "no coverage rendered here" is a normal state (a
// tile the user's history has never touched, or one still waiting on render_fog), not an
// error condition. Shared by handleFogTile (object_key) and handleHeatmapTile
// (heatmap_object_key) — both are now plain precomputed-aggregate lookups, just against
// different columns of the same row.
func loadMaskOrBlank(ctx context.Context, store *storage.Store, objectKey *string) (*image.Gray, error) {
	if objectKey == nil {
		return image.NewGray(image.Rect(0, 0, fog.TileSize, fog.TileSize)), nil
	}
	obj, err := store.Get(ctx, *objectKey)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	decoded, err := png.Decode(obj)
	if err != nil {
		return nil, err
	}
	gray, ok := decoded.(*image.Gray)
	if !ok {
		return nil, fmt.Errorf("tile %q is not grayscale", *objectKey)
	}
	return gray, nil
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
