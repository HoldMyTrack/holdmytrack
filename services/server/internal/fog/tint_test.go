package fog

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodeMask(t *testing.T, m image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustTile(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func decodeTile(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("served tile doesn't decode: %v", err)
	}
	return img
}

// rampMask holds every mask value once, 0..255 left to right.
func rampMask() *image.Gray {
	m := image.NewGray(image.Rect(0, 0, 256, 1))
	for v := range 256 {
		m.Pix[v] = uint8(v)
	}
	return m
}

// The served tile draws the same as colouring every pixel did: the veil (or the heat ramp)
// premultiplied by its alpha, to within rounding, for every one of the 256 mask values.
func TestTintMatchesPerPixelColouring(t *testing.T) {
	mask := rampMask()
	for _, tc := range []struct {
		name  string
		tile  []byte
		color func(v uint8) color.NRGBA
	}{
		{"fog light", mustTile(FogTile(encodeMask(t, mask), LightVeil)), func(v uint8) color.NRGBA {
			return color.NRGBA{R: LightVeil.R, G: LightVeil.G, B: LightVeil.B, A: uint8(LightVeil.Opacity * float64(255-v))}
		}},
		{"fog dark", mustTile(FogTile(encodeMask(t, mask), DarkVeil)), func(v uint8) color.NRGBA {
			return color.NRGBA{R: DarkVeil.R, G: DarkVeil.G, B: DarkVeil.B, A: uint8(DarkVeil.Opacity * float64(255-v))}
		}},
		{"heatmap", mustTile(HeatmapTile(encodeMask(t, mask))), heatmapNRGBA},
	} {
		img := decodeTile(t, tc.tile)
		if _, ok := img.(*image.Paletted); !ok {
			t.Errorf("%s: decoded as %T, want a palette image", tc.name, img)
		}
		for v := range 256 {
			got := color.RGBAModel.Convert(img.At(v, 0)).(color.RGBA)
			want := color.RGBAModel.Convert(tc.color(uint8(v))).(color.RGBA)
			for i, d := range []int{int(got.R) - int(want.R), int(got.G) - int(want.G), int(got.B) - int(want.B), int(got.A) - int(want.A)} {
				if d > 1 || d < -1 {
					t.Fatalf("%s: value %d channel %d = %+v, want %+v", tc.name, v, i, got, want)
				}
			}
		}
	}
}

// No stored mask: Fog is the full veil, Heatmap fully transparent, at the full tile size.
func TestTintBlank(t *testing.T) {
	fog := decodeTile(t, mustTile(FogTile(nil, LightVeil)))
	if b := fog.Bounds(); b.Dx() != TileSize || b.Dy() != TileSize {
		t.Fatalf("blank tile is %v, want %d×%d", b, TileSize, TileSize)
	}
	if _, _, _, a := fog.At(10, 10).RGBA(); a>>8 != uint32(LightVeil.Opacity*255) {
		t.Errorf("blank Fog alpha = %d, want the veil's", a>>8)
	}
	if _, _, _, a := decodeTile(t, mustTile(HeatmapTile(nil))).At(10, 10).RGBA(); a != 0 {
		t.Errorf("blank Heatmap alpha = %d, want 0", a)
	}
}

// A stored mask that isn't 8-bit grayscale is still served, by way of a re-encode.
func TestTintAcceptsAnyMask(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 1))
	rgba.Set(1, 0, color.White)
	img := decodeTile(t, mustTile(FogTile(encodeMask(t, rgba), LightVeil)))
	if _, _, _, a := img.At(1, 0).RGBA(); a != 0 {
		t.Errorf("covered pixel alpha = %d, want 0", a)
	}
	if _, err := FogTile([]byte("not a png"), LightVeil); err == nil {
		t.Error("garbage mask: no error")
	}
}
