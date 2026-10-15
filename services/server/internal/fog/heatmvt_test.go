package fog

import (
	"encoding/binary"
	"math"
	"testing"
)

// field is one decoded protobuf field: its number, and its varint or its bytes.
type field struct {
	num   int
	v     uint64
	bytes []byte
}

// fields reads a protobuf message's fields, the wire types a vector tile uses.
func fields(t *testing.T, b []byte) []field {
	t.Helper()
	var out []field
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		b = b[n:]
		f := field{num: int(key >> 3)}
		switch key & 7 {
		case 0:
			f.v, n = binary.Uvarint(b)
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			f.bytes, b = b[n:n+int(l)], b[n+int(l):]
		case 5:
			f.v, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		default:
			t.Fatalf("unexpected wire type %d", key&7)
		}
		out = append(out, f)
	}
	return out
}

func varints(b []byte) []uint64 {
	var out []uint64
	for len(b) > 0 {
		v, n := binary.Uvarint(b)
		out, b = append(out, v), b[n:]
	}
	return out
}

func unzigzag(v uint64) int { return int(v>>1) ^ -int(v&1) }

// The tile decodes as the format reads it: one "heat" layer at a 4096 extent, a point per
// HeatPoint where it was put, carrying its weight; equal weights share one value.
func TestEncodeHeatPoints(t *testing.T) {
	points := []HeatPoint{{X: 32, Y: 96, W: 0.25}, {X: 4064, Y: 32, W: 1}, {X: 2080, Y: 2080, W: 0.25}}
	tile := fields(t, EncodeHeatPoints(points))
	if len(tile) != 1 || tile[0].num != 3 {
		t.Fatalf("tile fields %v, want one layer", tile)
	}
	var name, key string
	var version, extent uint64
	var values []float32
	type feat struct {
		x, y int
		vi   uint64
	}
	var feats []feat
	for _, f := range fields(t, tile[0].bytes) {
		switch f.num {
		case 15:
			version = f.v
		case 1:
			name = string(f.bytes)
		case 3:
			key = string(f.bytes)
		case 5:
			extent = f.v
		case 4:
			v := fields(t, f.bytes)
			if len(v) != 1 || v[0].num != 2 {
				t.Fatalf("value %v, want a float", v)
			}
			values = append(values, math.Float32frombits(uint32(v[0].v)))
		case 2:
			var ft feat
			for _, g := range fields(t, f.bytes) {
				switch g.num {
				case 2:
					tags := varints(g.bytes)
					if len(tags) != 2 || tags[0] != 0 {
						t.Fatalf("tags %v, want key 0 and a value", tags)
					}
					ft.vi = tags[1]
				case 3:
					if g.v != 1 {
						t.Errorf("geometry type %d, want POINT", g.v)
					}
				case 4:
					cmd := varints(g.bytes)
					if len(cmd) != 3 || cmd[0] != 1|1<<3 {
						t.Fatalf("geometry %v, want one MoveTo", cmd)
					}
					ft.x, ft.y = unzigzag(cmd[1]), unzigzag(cmd[2])
				}
			}
			feats = append(feats, ft)
		}
	}
	if version != 2 || name != "heat" || key != "w" || extent != 4096 {
		t.Errorf("layer v%d %q key %q extent %d, want v2 \"heat\" key \"w\" extent 4096", version, name, key, extent)
	}
	if len(values) != 2 {
		t.Errorf("values %v, want the two distinct weights", values)
	}
	if len(feats) != len(points) {
		t.Fatalf("%d features, want %d", len(feats), len(points))
	}
	for i, p := range points {
		f := feats[i]
		if f.x != p.X || f.y != p.Y || float64(values[f.vi]) != p.W {
			t.Errorf("feature %d at (%d, %d) weighing %v, want (%d, %d) weighing %v", i, f.x, f.y, values[f.vi], p.X, p.Y, p.W)
		}
	}
	if EncodeHeatPoints(nil) != nil {
		t.Error("no points made bytes, want an empty tile")
	}
}

func BenchmarkEncodeHeatPoints(b *testing.B) {
	points := make([]HeatPoint, 0, heatCellsPerSide*heatCellsPerSide)
	for y := range heatCellsPerSide {
		for x := range heatCellsPerSide {
			points = append(points, HeatPoint{X: x*64 + 32, Y: y*64 + 32, W: float64((x+y)%10) / 10})
		}
	}
	for b.Loop() {
		EncodeHeatPoints(points)
	}
}
