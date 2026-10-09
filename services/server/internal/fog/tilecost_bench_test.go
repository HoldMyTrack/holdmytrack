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

func benchStroke() *image.Gray {
	m := image.NewGray(image.Rect(0, 0, TileSize, TileSize))
	for i := 0; i < TileSize; i++ {
		for w := -6; w <= 6; w++ {
			if y := i/2 + 100 + w; y >= 0 && y < TileSize {
				m.Pix[y*m.Stride+i] = 255
			}
		}
	}
	return m
}

func BenchmarkCompositeFog(b *testing.B) {
	masks := []*image.Gray{benchStroke(), benchStroke(), benchStroke()}
	for b.Loop() {
		compositeFogMask(masks)
	}
}

func BenchmarkCompositeHeatmap(b *testing.B) {
	masks := []*image.Gray{benchStroke(), benchStroke(), benchStroke()}
	for b.Loop() {
		compositeHeatmapMask(masks, 8)
	}
}

func BenchmarkDownsample(b *testing.B) {
	c := [4]*image.Gray{benchStroke(), benchStroke(), benchStroke(), benchStroke()}
	for b.Loop() {
		downsampleQuadrants(c)
	}
}

func BenchmarkEncodeMask(b *testing.B) {
	m := compositeFogMask([]*image.Gray{benchStroke()})
	var buf bytes.Buffer
	for b.Loop() {
		buf.Reset()
		png.Encode(&buf, m)
	}
}

func BenchmarkDecodeMask(b *testing.B) {
	var buf bytes.Buffer
	png.Encode(&buf, compositeFogMask([]*image.Gray{benchStroke()}))
	data := buf.Bytes()
	for b.Loop() {
		png.Decode(bytes.NewReader(data))
	}
}
