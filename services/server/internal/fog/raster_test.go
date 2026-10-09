package fog

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

func TestFogTileVeils(t *testing.T) {
	mask := image.NewGray(image.Rect(0, 0, 2, 1))
	mask.SetGray(1, 0, color.Gray{Y: 255}) // (0,0) unexplored, (1,0) fully covered

	for _, tc := range []struct {
		theme string
		veil  Veil
	}{
		{"", LightVeil},
		{"light", LightVeil},
		{"dark", DarkVeil},
	} {
		veil := VeilForTheme(tc.theme)
		if veil != tc.veil {
			t.Fatalf("VeilForTheme(%q) = %+v, want %+v", tc.theme, veil, tc.veil)
		}
		out := decodeTile(t, mustTile(FogTile(encodeMask(t, mask), veil)))

		fogged := color.NRGBAModel.Convert(out.At(0, 0)).(color.NRGBA)
		want := color.NRGBA{R: veil.R, G: veil.G, B: veil.B, A: uint8(veil.Opacity * 255)}
		if fogged != want {
			t.Errorf("theme %q: unexplored pixel = %+v, want %+v", tc.theme, fogged, want)
		}
		if _, _, _, a := out.At(1, 0).RGBA(); a != 0 {
			t.Errorf("theme %q: covered pixel alpha = %d, want fully transparent", tc.theme, a)
		}
	}
}

// A one-pixel line at z14 — a track thinner than a pixel, far out — stays fully covered and at
// least 3 px wide however many levels it's downsampled through, instead of fading as a mean
// would thin it out. Each level feeds the next the way renderPyramidLevel does, the line's tile
// as the top-left child.
func TestDownsampleKeepsThinLinesVisible(t *testing.T) {
	tile := blankTile()
	for x := 0; x < TileSize; x++ {
		tile.Pix[200*TileSize+x] = 255 // a horizontal line, one pixel high
	}
	for level := 1; level <= 6; level++ {
		tile = downsampleQuadrants([4]*image.Gray{tile, blankTile(), blankTile(), blankTile()})
		width, full := 0, false
		for y := 0; y < TileSize; y++ {
			if v := tile.Pix[y*TileSize+1]; v > 0 { // near the left edge: each level shrinks the line into the top-left quarter
				width++
				full = full || v == 255
			}
		}
		if !full || width < 3 {
			t.Fatalf("after %d levels the line is %d px wide, full strength %v; want >= 3 px at 255", level, width, full)
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

// Nothing is drawn where nothing was visited; anywhere visited at all — even the faintest
// single pass on an account with a busy home tile — is plainly visible, and the ramp only
// gets more opaque as it heats up.
func TestHeatmapRampVisibleWhereVisited(t *testing.T) {
	if a := heatmapNRGBA(0).A; a != 0 {
		t.Errorf("intensity 0: alpha %d, want 0", a)
	}
	for _, i := range []uint8{5, 8, 20} {
		if a := heatmapNRGBA(i).A; a < 160 {
			t.Errorf("intensity %d (a single pass): alpha %d, want >= 160", i, a)
		}
	}
	prev := uint8(0)
	for i := 0; i <= 255; i++ {
		a := heatmapNRGBA(uint8(i)).A
		if a < prev {
			t.Fatalf("alpha drops from %d to %d at intensity %d", prev, a, i)
		}
		prev = a
	}
}

// boxBlurReference is the direct per-pixel box blur boxBlur replaced: every pixel the
// integer mean of the in-bounds pixels of the square around it.
func boxBlurReference(src *image.Gray, radius int) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewGray(src.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum, n int
			for sy := max(y-radius, 0); sy <= min(y+radius, h-1); sy++ {
				for sx := max(x-radius, 0); sx <= min(x+radius, w-1); sx++ {
					sum += int(src.Pix[sy*src.Stride+sx])
					n++
				}
			}
			out.Pix[y*out.Stride+x] = uint8(sum / n)
		}
	}
	return out
}

// The two-pass blur is byte for byte the direct one, edges included, for any radius — even
// one wider than the image.
func TestBoxBlurMatchesTheDirectMean(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, size := range []image.Point{{TileSize, TileSize}, {7, 5}, {1, 1}, {3, 9}} {
		src := image.NewGray(image.Rect(0, 0, size.X, size.Y))
		for i := range src.Pix {
			if rng.IntN(4) == 0 { // mostly empty, like a mask, with full-strength strokes
				src.Pix[i] = uint8(rng.IntN(256))
			}
		}
		for _, r := range []int{1, 2, featherPx, 5, 12} {
			got, want := boxBlur(src, r), boxBlurReference(src, r)
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("%v radius %d: differs from the direct mean", size, r)
			}
		}
	}
}

// downsampleReference is the pyramid step downsampleQuadrants replaced: the four children
// copied into one double-size image, each 2x2 block's brightest pixel, then a 3x3 max filter
// pixel by pixel.
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
	out := image.NewGray(pooled.Bounds())
	for y := 0; y < TileSize; y++ {
		for x := 0; x < TileSize; x++ {
			var v uint8
			for sy := max(y-1, 0); sy <= min(y+1, TileSize-1); sy++ {
				for sx := max(x-1, 0); sx <= min(x+1, TileSize-1); sx++ {
					v = max(v, pooled.Pix[sy*TileSize+sx])
				}
			}
			out.Pix[y*TileSize+x] = v
		}
	}
	return out
}

// The pyramid step is byte for byte the copy-pool-dilate it replaced, children of every kind.
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
