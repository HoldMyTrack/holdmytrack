package httpapi

import (
	"context"
	"fmt"
	"image"
	"image/png"

	"github.com/fitmap/fitmap/services/server/internal/fog"
	"github.com/fitmap/fitmap/services/server/internal/storage"
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
