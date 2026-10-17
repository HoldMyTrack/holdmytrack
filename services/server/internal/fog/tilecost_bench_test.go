package fog

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func benchStroke() *image.Gray {
	m := image.NewGray(image.Rect(0, 0, TileSize, TileSize))
	for i := 0; i < TileSize; i++ {
		for w := -1; w <= 1; w++ { // a stroke ~3 px wide, as strokeRadiusPx draws one
			if y := i/2 + TileSize/5 + w; y >= 0 && y < TileSize {
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

func BenchmarkHeatCounts(b *testing.B) {
	masks := []*image.Gray{benchStroke(), benchStroke(), benchStroke()}
	for b.Loop() {
		heatCounts(masks)
	}
}

func BenchmarkDownsampleCounts(b *testing.B) {
	c := [4]*image.Gray16{heatCounts([]*image.Gray{benchStroke()}), blankCounts(), blankCounts(), blankCounts()}
	for b.Loop() {
		downsampleCounts(c)
	}
}

// A heat-points tile from a busy count tile: the decode, then the points.
func BenchmarkHeatPoints(b *testing.B) {
	masks := make([]*image.Gray, 40)
	for i := range masks {
		masks[i] = benchStroke()
	}
	png, err := encodeTilePNG(heatCounts(masks))
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		counts, err := DecodeCounts(png)
		if err != nil {
			b.Fatal(err)
		}
		HeatPoints(counts, 0, 0, 40)
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
