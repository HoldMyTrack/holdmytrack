package tilemath

import (
	"testing"
)

// lonLatFromWorldPixel inverts WorldPixel — used only by this test to construct a point at an
// exact, known tile-local pixel offset, the same way a fixture would hand-pick a lat/lon and
// then have to trust the projection landed where intended.
func lonLatFromWorldPixel(wx, wy float64, zoom int, tileSize float64) (lon, lat float64) {
	n := 1 << uint(zoom)
	lon = wx/(tileSize*float64(n))*360 - 180
	lat = tileYToLat(wy/tileSize, float64(n))
	return lon, lat
}

func tileSet(tiles [][2]int) map[[2]int]bool {
	out := make(map[[2]int]bool, len(tiles))
	for _, t := range tiles {
		out[t] = true
	}
	return out
}

func TestSegmentTilesBufferedInteriorPointNoNeighbors(t *testing.T) {
	const zoom = 14
	const tileSize = 512.0
	const margin = 9.0

	lon, lat := lonLatFromWorldPixel(100*tileSize+256, 100*tileSize+256, zoom, tileSize)
	got := tileSet(SegmentTilesBuffered(lon, lat, lon, lat, zoom, margin, tileSize))

	want := map[[2]int]bool{{100, 100}: true}
	if len(got) != len(want) || !got[[2]int{100, 100}] {
		t.Fatalf("interior point pulled in unexpected neighbors: got %v, want %v", got, want)
	}
}

func TestSegmentTilesBufferedNearEdgeAddsNeighbor(t *testing.T) {
	const zoom = 14
	const tileSize = 512.0
	const margin = 9.0

	// localX = 509, well within margin (9px) of the tile's right edge at 512.
	lon, lat := lonLatFromWorldPixel(100*tileSize+509, 100*tileSize+256, zoom, tileSize)
	got := tileSet(SegmentTilesBuffered(lon, lat, lon, lat, zoom, margin, tileSize))

	for _, want := range [][2]int{{100, 100}, {101, 100}} {
		if !got[want] {
			t.Errorf("expected tile %v in buffered set, got %v", want, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("expected exactly 2 tiles, got %v", got)
	}
}

func TestSegmentTilesBufferedNearCornerAddsDiagonal(t *testing.T) {
	const zoom = 14
	const tileSize = 512.0
	const margin = 9.0

	// localX = localY = 509: within margin of both the right and bottom edges, so the
	// stroke's drawn width can reach the right, bottom, AND bottom-right diagonal neighbor.
	lon, lat := lonLatFromWorldPixel(100*tileSize+509, 100*tileSize+509, zoom, tileSize)
	got := tileSet(SegmentTilesBuffered(lon, lat, lon, lat, zoom, margin, tileSize))

	for _, want := range [][2]int{{100, 100}, {101, 100}, {100, 101}, {101, 101}} {
		if !got[want] {
			t.Errorf("expected tile %v in buffered set, got %v", want, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected exactly 4 tiles, got %v", got)
	}
}

// TestSegmentTilesBufferedHuggingBoundary reproduces docs/KNOWN_ISSUES.md's reported defect:
// a segment that runs the length of a tile boundary, both endpoints inside the same tile but
// close enough to its edge that the drawn stroke spills into the neighbor — unbuffered
// SegmentTiles (and the old computeTouchedTiles) never marks that neighbor touched at all.
func TestSegmentTilesBufferedHuggingBoundary(t *testing.T) {
	const zoom = 14
	const tileSize = 512.0
	const margin = 9.0

	lon1, lat1 := lonLatFromWorldPixel(100*tileSize+509, 100*tileSize+50, zoom, tileSize)
	lon2, lat2 := lonLatFromWorldPixel(100*tileSize+509, 100*tileSize+450, zoom, tileSize)

	unbuffered := tileSet(SegmentTiles(lon1, lat1, lon2, lat2, zoom))
	if len(unbuffered) != 1 || !unbuffered[[2]int{100, 100}] {
		t.Fatalf("expected unbuffered segment to stay within the one tile, got %v", unbuffered)
	}

	buffered := tileSet(SegmentTilesBuffered(lon1, lat1, lon2, lat2, zoom, margin, tileSize))
	for _, want := range [][2]int{{100, 100}, {101, 100}} {
		if !buffered[want] {
			t.Errorf("expected tile %v in buffered set, got %v", want, buffered)
		}
	}
	if len(buffered) != 2 {
		t.Errorf("expected exactly 2 tiles, got %v", buffered)
	}
}

func TestSegmentTilesBufferedFarFromEdgeNoNeighbors(t *testing.T) {
	const zoom = 14
	const tileSize = 512.0
	const margin = 9.0

	lon1, lat1 := lonLatFromWorldPixel(100*tileSize+256, 100*tileSize+50, zoom, tileSize)
	lon2, lat2 := lonLatFromWorldPixel(100*tileSize+256, 100*tileSize+450, zoom, tileSize)

	got := tileSet(SegmentTilesBuffered(lon1, lat1, lon2, lat2, zoom, margin, tileSize))
	want := map[[2]int]bool{{100, 100}: true}
	if len(got) != len(want) || !got[[2]int{100, 100}] {
		t.Fatalf("mid-tile segment pulled in unexpected neighbors: got %v", got)
	}
}
