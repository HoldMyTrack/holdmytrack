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
