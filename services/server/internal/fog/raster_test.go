package fog

import (
	"image"
	"image/color"
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
