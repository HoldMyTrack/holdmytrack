package fog

import (
	"image"
	"image/color"
	"testing"
)

func TestRenderFogPNGVeils(t *testing.T) {
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
		out := RenderFogPNG(mask, veil)

		fogged := out.RGBAAt(0, 0)
		wantA := uint8(veil.Opacity * 255)
		if fogged.A != wantA {
			t.Errorf("theme %q: unexplored alpha = %d, want %d", tc.theme, fogged.A, wantA)
		}
		// Un-premultiply to compare against the veil colour (±1 for rounding).
		got := [3]float64{float64(fogged.R), float64(fogged.G), float64(fogged.B)}
		want := [3]uint8{veil.R, veil.G, veil.B}
		for i := range got {
			if d := got[i]*255/float64(fogged.A) - float64(want[i]); d > 1.5 || d < -1.5 {
				t.Errorf("theme %q: channel %d = %v, want %d", tc.theme, i, got[i]*255/float64(fogged.A), want[i])
			}
		}

		if cleared := out.RGBAAt(1, 0); cleared != (color.RGBA{}) {
			t.Errorf("theme %q: covered pixel = %+v, want fully transparent", tc.theme, cleared)
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
