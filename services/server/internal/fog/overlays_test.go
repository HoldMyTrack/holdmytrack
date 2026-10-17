package fog

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/HoldMyTrack/platform/services/server/internal/mapstyle"
)

// servedOverlays decodes what this package's tiles are drawn by: the overlays in the served
// style (apps/web/src/map/coverageStyle.ts).
type servedOverlays struct {
	Sources map[string]struct {
		MaxZoom int `json:"maxzoom"`
	} `json:"sources"`
	Layers []struct {
		ID    string                     `json:"id"`
		Paint map[string]json.RawMessage `json:"paint"`
	} `json:"layers"`
}

// TestServedOverlaysMatchTheTiles holds the clients' drawing to what the tiles assume: the
// heat circles' radius, which weighNeighbours weights each point against, and the last zoom of
// each tile kind.
func TestServedOverlaysMatchTheTiles(t *testing.T) {
	for _, flavor := range mapstyle.Flavors() {
		doc, err := mapstyle.Document(flavor, "en", "", mapstyle.Satellite{})
		if err != nil {
			t.Fatal(err)
		}
		var style struct {
			Metadata struct {
				Overlays servedOverlays `json:"holdmytrack:overlays"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(doc, &style); err != nil {
			t.Fatal(err)
		}
		o := style.Metadata.Overlays
		if got := o.Sources["fog-vector"].MaxZoom; got != Zoom {
			t.Errorf("%s: fog-vector maxzoom = %d, want %d", flavor, got, Zoom)
		}
		if got := o.Sources["heat-points"].MaxZoom; got != HeatPointsMaxZoom {
			t.Errorf("%s: heat-points maxzoom = %d, want %d", flavor, got, HeatPointsMaxZoom)
		}
		var radius []any
		for _, l := range o.Layers {
			if l.ID == "heat-points-circles" {
				if err := json.Unmarshal(l.Paint["heatmap-radius"], &radius); err != nil {
					t.Fatalf("%s: heatmap-radius: %v", flavor, err)
				}
			}
		}
		// ["interpolate", ["exponential", 2], ["zoom"], 0, r, …]: r px at a tile's own zoom, where
		// a 512 px tile holds heatCellsPerSide cells.
		if len(radius) < 5 {
			t.Fatalf("%s: heatmap-radius = %v", flavor, radius)
		}
		cells := radius[4].(float64) / (512.0 / heatCellsPerSide)
		if math.Abs(cells-heatRadiusCells) > 1e-9 {
			t.Errorf("%s: heat circles are %v cells across, weighNeighbours assumes %v", flavor, cells, heatRadiusCells)
		}
	}
}
