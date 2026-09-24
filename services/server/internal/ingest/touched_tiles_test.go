package ingest

import (
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

func touchedTileSet(tiles [][2]int) map[[2]int]bool {
	out := make(map[[2]int]bool, len(tiles))
	for _, t := range tiles {
		out[t] = true
	}
	return out
}

// TestComputeTouchedTilesBuffersStrokeWidth reproduces docs/KNOWN_ISSUES.md's reported
// defect: a trajectory that stays within one z14 tile but hugs its boundary closely enough
// that the rendered stroke (fog.TileMarginPx wide) spills into the neighboring tile.
// computeTouchedTiles has to mark that neighbor touched too, or RenderActivityMasks never
// renders a mask there and the overflow is silently clipped at the canvas edge instead.
func TestComputeTouchedTilesBuffersStrokeWidth(t *testing.T) {
	const zoom = FogZoom
	minLon, minLat, maxLon, maxLat := tilemath.TileBounds(100, 100, zoom)

	// localX = 509 of 512, within fog.TileMarginPx (9px) of the tile's right edge — lon
	// interpolates linearly within a tile, so this is exact regardless of latitude.
	frac := 509.0 / 512.0
	lon := minLon + frac*(maxLon-minLon)
	lat1 := minLat + 0.1*(maxLat-minLat)
	lat2 := minLat + 0.9*(maxLat-minLat)

	points := []parse.Point{
		{Lat: lat1, Lon: lon, Time: time.Unix(0, 0)},
		{Lat: lat2, Lon: lon, Time: time.Unix(1, 0)},
	}

	got := touchedTileSet(computeTouchedTiles(points, zoom))
	for _, want := range [][2]int{{100, 100}, {101, 100}} {
		if !got[want] {
			t.Errorf("expected tile %v touched (margin %v px), got %v", want, fog.TileMarginPx, got)
		}
	}
}

// TestComputeTouchedTilesFarFromEdgeStaysSingleTile guards the buffer from over-triggering:
// a trajectory comfortably inside a tile shouldn't pull in neighbors it never gets near.
func TestComputeTouchedTilesFarFromEdgeStaysSingleTile(t *testing.T) {
	const zoom = FogZoom
	minLon, minLat, maxLon, maxLat := tilemath.TileBounds(100, 100, zoom)

	lon := minLon + 0.5*(maxLon-minLon)
	lat1 := minLat + 0.1*(maxLat-minLat)
	lat2 := minLat + 0.9*(maxLat-minLat)

	points := []parse.Point{
		{Lat: lat1, Lon: lon, Time: time.Unix(0, 0)},
		{Lat: lat2, Lon: lon, Time: time.Unix(1, 0)},
	}

	got := touchedTileSet(computeTouchedTiles(points, zoom))
	if len(got) != 1 || !got[[2]int{100, 100}] {
		t.Errorf("expected only tile {100,100}, got %v", got)
	}
}
