package fog

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// BenchmarkFogTile is serving one stored z14 mask — a couple of strokes across an otherwise
// empty tile — as its Fog tile.
func BenchmarkFogTile(b *testing.B) {
	m := image.NewGray(image.Rect(0, 0, TileSize, TileSize))
	for i := 0; i < TileSize; i++ {
		for w := -6; w <= 6; w++ {
			if y := i/2 + 100 + w; y >= 0 && y < TileSize {
				m.Pix[y*m.Stride+i] = 255
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		b.Fatal(err)
	}
	mask := buf.Bytes()
	for b.Loop() {
		if _, err := FogTile(mask, LightVeil); err != nil {
			b.Fatal(err)
		}
	}
}
