package fog

import (
	"bytes"
	"errors"
	"image"
	"math/rand/v2"
	"testing"
)

// A stored PNG of another size than TileSize is refused, not handed to a raster that indexes a
// TileSize square; one of TileSize decodes as before.
func TestDecodeGrayRefusesAnotherSize(t *testing.T) {
	for _, size := range []int{TileSize / 2, TileSize, TileSize * 2} {
		b, err := encodeTilePNG(image.NewGray(image.Rect(0, 0, size, size)))
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeGray(bytes.NewReader(b))
		if want := size != TileSize; errors.Is(err, errTileSize) != want {
			t.Errorf("%d px: err = %v, want refused %v", size, err, want)
		}
	}
}

// A one-pixel line at z14 — a track thinner than a pixel, far out — stays fully covered however
// many levels it's downsampled through, instead of fading as a mean would thin it out. Each
// level feeds the next the way renderPyramidLevel does, the line's tile as the top-left child.
func TestDownsampleKeepsThinLinesVisible(t *testing.T) {
	tile := blankTile()
	for x := 0; x < TileSize; x++ {
		tile.Pix[(TileSize/3)*TileSize+x] = 255 // a horizontal line, one pixel high
	}
	for level := 1; level <= 6; level++ {
		tile = downsampleQuadrants([4]*image.Gray{tile, blankTile(), blankTile(), blankTile()})
		full := false
		for y := 0; y < TileSize; y++ {
			// Near the left edge: each level shrinks the line into the top-left quarter.
			full = full || tile.Pix[y*TileSize] == 255
		}
		if !full {
			t.Fatalf("after %d levels the line is gone or faded; want it at full strength", level)
		}
	}
}

// Nothing visited stays nothing: an all-blank block downsamples to an all-blank tile.
func TestDownsampleKeepsBlankBlank(t *testing.T) {
	out := downsampleQuadrants([4]*image.Gray{blankTile(), blankTile(), blankTile(), blankTile()})
	for i, v := range out.Pix {
		if v != 0 {
			t.Fatalf("pixel %d = %d, want 0", i, v)
		}
	}
}

// downsampleReference is the pyramid step written plainly: the four children copied into one
// double-size image, then each 2x2 block's brightest pixel.
func downsampleReference(children [4]*image.Gray) *image.Gray {
	big := image.NewGray(image.Rect(0, 0, TileSize*2, TileSize*2))
	for q, c := range children {
		ox, oy := (q%2)*TileSize, (q/2)*TileSize
		for y := 0; y < TileSize; y++ {
			copy(big.Pix[(oy+y)*2*TileSize+ox:], c.Pix[y*TileSize:(y+1)*TileSize])
		}
	}
	pooled := image.NewGray(image.Rect(0, 0, TileSize, TileSize))
	for y := 0; y < TileSize; y++ {
		for x := 0; x < TileSize; x++ {
			x2, y2 := x*2, y*2
			pooled.Pix[y*TileSize+x] = max(big.Pix[y2*2*TileSize+x2], big.Pix[y2*2*TileSize+x2+1],
				big.Pix[(y2+1)*2*TileSize+x2], big.Pix[(y2+1)*2*TileSize+x2+1])
		}
	}
	return pooled
}

// The pyramid step is byte for byte the plain copy-and-pool, children of every kind.
func TestDownsampleMatchesTheReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	var children [4]*image.Gray
	for i := range children {
		children[i] = blankTile()
		if i == 2 {
			continue // a blank sibling, as at the edge of anyone's history
		}
		for j := range children[i].Pix {
			if rng.IntN(50) == 0 {
				children[i].Pix[j] = uint8(rng.IntN(256))
			}
		}
	}
	if got, want := downsampleQuadrants(children), downsampleReference(children); !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("differs from the reference pyramid step")
	}
}

// A track recorded standing still for stretches — GPS jitter of a few centimetres, repeated
// points — still draws. At 128 px those moves are a small fraction of a pixel, and a stroke
// through them used to come out empty: the rasterizer works in 1/64-pixel fixed point, where
// such a segment has no length.
func TestActivityMaskSurvivesJitter(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	var line []pixelPoint
	x := 5.0
	for x < TileSize-5 {
		if rng.IntN(3) == 0 {
			x += 0.02 // walking on, a few centimetres at a time
		}
		line = append(line, pixelPoint{x: x + rng.Float64()*0.01, y: TileSize/2 + rng.Float64()*0.01})
		if rng.IntN(10) == 0 {
			line = append(line, line[len(line)-1]) // the same fix twice
		}
	}
	m := renderActivityMask(line)
	lit := 0
	for _, v := range m.Pix {
		if v > 128 {
			lit++
		}
	}
	if lit < TileSize-10 {
		t.Fatalf("a %d-point walk across the tile lit %d pixels, want a line of at least %d", len(line), lit, TileSize-10)
	}
}
