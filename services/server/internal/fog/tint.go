package fog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"sync"
)

// A served Fog or Heatmap tile is its stored mask with a colour for each of the mask's 256
// values: the veil's colour at an alpha falling with coverage, or the heat ramp. So it is
// served as exactly that — the stored PNG rewritten as an 8-bit palette PNG over the same
// pixel bytes. An 8-bit grayscale PNG and an 8-bit palette PNG store their pixels the same
// way, one byte each, so only the header's colour type changes and a palette (PLTE) and its
// alphas (tRNS) go in before the image data; the compressed image data itself is copied as
// it is. Decoding the mask, colouring 512×512 pixels and encoding an RGBA PNG on every
// request was what saturated the API's CPU in the 2026-10-08 load test, about 6 ms a tile on
// a fast laptop, five of them the encode; this is a few microseconds.

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// FogTile is the served Fog tile for a stored coverage mask (nil: no row, fully fogged):
// the veil's colour, at alpha = veil opacity × (255 − coverage).
func FogTile(maskPNG []byte, veil Veil) ([]byte, error) {
	return tint(maskPNG, fogPalette(veil))
}

// HeatmapTile is the served Heatmap tile for a stored intensity mask (nil: no heat, fully
// transparent), coloured along heatmapRamp.
func HeatmapTile(maskPNG []byte) ([]byte, error) {
	return tint(maskPNG, heatmapPalette())
}

type palette struct {
	plte [256 * 3]byte
	trns [256]byte
}

var (
	fogPalettes   sync.Map // Veil -> *palette
	heatmapPalOne = sync.OnceValue(func() *palette {
		var p palette
		for v := range 256 {
			p.set(v, heatmapNRGBA(uint8(v)))
		}
		return &p
	})
)

func heatmapPalette() *palette { return heatmapPalOne() }

func fogPalette(veil Veil) *palette {
	if p, ok := fogPalettes.Load(veil); ok {
		return p.(*palette)
	}
	var p palette
	for v := range 256 {
		p.set(v, color.NRGBA{R: veil.R, G: veil.G, B: veil.B, A: uint8(veil.Opacity * float64(255-v))})
	}
	actual, _ := fogPalettes.LoadOrStore(veil, &p)
	return actual.(*palette)
}

func (p *palette) set(i int, c color.NRGBA) {
	p.plte[i*3], p.plte[i*3+1], p.plte[i*3+2] = c.R, c.G, c.B
	p.trns[i] = c.A
}

// blankMaskPNG is the mask of a tile with no fog_tiles row (or no render on that side yet):
// all zero, encoded once.
var blankMaskPNG = sync.OnceValue(func() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, TileSize, TileSize))); err != nil {
		panic(err) // encoding an in-memory image to a bytes.Buffer can't fail
	}
	return buf.Bytes()
})

var errNotGray8 = errors.New("not an 8-bit grayscale PNG")

// tint rewrites an 8-bit grayscale PNG as an 8-bit palette PNG with p. A mask that isn't one
// (never written by this package, but object storage holds whatever was put there) is decoded
// and re-encoded as one first.
func tint(maskPNG []byte, p *palette) ([]byte, error) {
	if maskPNG == nil {
		maskPNG = blankMaskPNG()
	}
	out, err := splicePalette(maskPNG, p)
	if !errors.Is(err, errNotGray8) {
		return out, err
	}
	img, derr := png.Decode(bytes.NewReader(maskPNG))
	if derr != nil {
		return nil, fmt.Errorf("decode mask: %w", derr)
	}
	gray := image.NewGray(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			gray.Set(x, y, img.At(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		return nil, fmt.Errorf("re-encode mask: %w", err)
	}
	return splicePalette(buf.Bytes(), p)
}

func splicePalette(src []byte, p *palette) ([]byte, error) {
	if !bytes.HasPrefix(src, pngSignature) {
		return nil, fmt.Errorf("%w: no PNG signature", errNotGray8)
	}
	out := make([]byte, 0, len(src)+len(p.plte)+len(p.trns)+24)
	out = append(out, pngSignature...)
	rest := src[len(pngSignature):]
	first := true
	for len(rest) > 0 {
		if len(rest) < 12 {
			return nil, errors.New("truncated PNG chunk")
		}
		n := binary.BigEndian.Uint32(rest[:4])
		if uint64(len(rest)) < 12+uint64(n) {
			return nil, errors.New("truncated PNG chunk")
		}
		typ := string(rest[4:8])
		data := rest[8 : 8+n]
		whole := rest[:12+n]
		rest = rest[12+n:]
		if first {
			// IHDR: width, height, bit depth, colour type, compression, filter, interlace.
			if typ != "IHDR" || n != 13 || data[8] != 8 || data[9] != 0 || data[12] != 0 {
				return nil, errNotGray8
			}
			ihdr := bytes.Clone(data)
			ihdr[9] = 3 // palette
			out = appendChunk(out, "IHDR", ihdr)
			out = appendChunk(out, "PLTE", p.plte[:])
			out = appendChunk(out, "tRNS", p.trns[:])
			first = false
			continue
		}
		if typ == "tRNS" || typ == "PLTE" {
			return nil, errNotGray8 // a grayscale tRNS means something else; take the slow path
		}
		out = append(out, whole...)
	}
	return out, nil
}

func appendChunk(out []byte, typ string, data []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	start := len(out)
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
}
