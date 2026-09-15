package httpapi

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"strings"

	"github.com/fitmap/fitmap/services/server/internal/fog"
	"github.com/fitmap/fitmap/services/server/internal/storage"
)

// parseMaskFilter extends parseActivityFilter's from/to parsing with "exclude" — the
// eye-icon/TYPE/DISTANCE hidden-activity-id set (apps/web's `mapHiddenIds`). Tracks and the
// list/summary endpoints don't need this: they hide client-side (tracks.ts's
// setHiddenTracks, a MapLibre layer filter). A raster tile has no per-feature identity to
// hide that way, so exclusion has to reach the server instead.
func parseMaskFilter(q url.Values) (fog.Filter, error) {
	af, err := parseActivityFilter(q)
	if err != nil {
		return fog.Filter{}, err
	}
	var exclude []string
	if v := q.Get("exclude"); v != "" {
		exclude = strings.Split(v, ",")
	}
	return fog.Filter{From: af.From, To: af.To, Exclude: exclude}, nil
}

// loadMaskOrBlank fetches and decodes a stored fog/heatmap tile mask, or returns a blank
// (all-zero) one when there is nothing stored yet — "no coverage rendered here" is a normal
// state (a tile the user's history has never touched, or one still waiting on render_fog),
// not an error condition. Shared by handleFogTile and handleHeatmapTile's unfiltered path;
// the filtered path (fog.RenderFilteredTile) does its own equivalent internally.
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
