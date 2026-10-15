package fog

import (
	"image"
	"math"
	"sort"
	"testing"
)

// decodedFog is a fog tile read back: its veil rings and edge lines, in coverage pixels.
type decodedFog struct {
	veil  [][]pt
	edges [][]pt
}

func decodeFogTile(t *testing.T, b []byte) decodedFog {
	t.Helper()
	tile := fields(t, b)
	if len(tile) != 1 || tile[0].num != 3 {
		t.Fatalf("tile fields %v, want one layer", tile)
	}
	var out decodedFog
	var values []string
	type feat struct {
		value, typ uint64
		cmds       []uint64
	}
	var feats []feat
	for _, f := range fields(t, tile[0].bytes) {
		switch f.num {
		case 1:
			if string(f.bytes) != "fog" {
				t.Errorf("layer %q, want fog", f.bytes)
			}
		case 5:
			if f.v != fogExtent {
				t.Errorf("extent %d, want %d", f.v, fogExtent)
			}
		case 4:
			values = append(values, string(fields(t, f.bytes)[0].bytes))
		case 2:
			var ft feat
			for _, g := range fields(t, f.bytes) {
				switch g.num {
				case 2:
					ft.value = varints(g.bytes)[1]
				case 3:
					ft.typ = g.v
				case 4:
					ft.cmds = varints(g.bytes)
				}
			}
			feats = append(feats, ft)
		}
	}
	const scale = float64(fogExtent / TileSize)
	for _, ft := range feats {
		var x, y int
		var parts [][]pt
		var cur []pt
		for i := 0; i < len(ft.cmds); {
			id, count := ft.cmds[i]&7, int(ft.cmds[i]>>3)
			i++
			switch id {
			case 1, 2:
				if id == 1 && cur != nil {
					parts = append(parts, cur)
					cur = nil
				}
				for range count {
					x += unzigzag(ft.cmds[i])
					y += unzigzag(ft.cmds[i+1])
					i += 2
					cur = append(cur, pt{float64(x) / scale, float64(y) / scale})
				}
			case 7:
				parts = append(parts, cur)
				cur = nil
			}
		}
		if cur != nil {
			parts = append(parts, cur)
		}
		switch values[ft.value] {
		case "veil":
			if ft.typ != 3 {
				t.Errorf("veil geometry type %d, want POLYGON", ft.typ)
			}
			out.veil = parts
		case "edge":
			if ft.typ != 2 {
				t.Errorf("edge geometry type %d, want LINESTRING", ft.typ)
			}
			out.edges = parts
		}
	}
	return out
}

// A tile with nothing cleared is all veil: one ring, the frame around the tile, and no edges.
func TestFogTileAllVeiled(t *testing.T) {
	got := decodeFogTile(t, EncodeFogTile(blankTile(), false))
	if len(got.veil) != 1 || len(got.edges) != 0 {
		t.Fatalf("%d veil rings, %d edges; want the frame alone", len(got.veil), len(got.edges))
	}
	if a := signedArea(got.veil[0]); a < TileSize*TileSize {
		t.Errorf("frame area %v, want the whole tile and more", a)
	}
}

// A road across the tile is a hole in the veil, with an edge along each side, as far from the
// road as the stroke reaches, carried just past the tile's sides.
func TestFogTileRoadAcross(t *testing.T) {
	const mid = TileSize / 2
	got := decodeFogTile(t, EncodeFogTile(lineMask(mid), false))
	if len(got.veil) != 2 {
		t.Fatalf("%d veil rings, want the frame and the road's hole", len(got.veil))
	}
	if signedArea(got.veil[0]) <= 0 || signedArea(got.veil[1]) >= 0 {
		t.Errorf("winding: frame %v, hole %v; want positive then negative", signedArea(got.veil[0]), signedArea(got.veil[1]))
	}
	if len(got.edges) != 2 {
		t.Fatalf("%d edges, want one along each side", len(got.edges))
	}
	for _, e := range got.edges {
		d := math.Abs(e[0].y - mid)
		if d < strokeRadiusPx-1 || d > strokeRadiusPx+1 {
			t.Errorf("edge %.2f px from the road, want about %v", d, strokeRadiusPx)
		}
		minX, maxX := e[0].x, e[0].x
		for _, p := range e {
			minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
			if math.Abs(p.y-e[0].y) > 0.5 {
				t.Errorf("edge wanders to y %.2f from %.2f", p.y, e[0].y)
			}
		}
		if minX > -0.4 || maxX < TileSize+0.4 {
			t.Errorf("edge runs %.2f..%.2f, want it carried just past both sides", minX, maxX)
		}
	}
}

// A cleared loop around an uncleared island: the island comes back as veil of its own, wound
// as an outer ring, inside the hole the loop cuts.
func TestFogTileIsland(t *testing.T) {
	const c, r = 64.0, 30.0
	var loop []pixelPoint
	for k := 0; k <= 64; k++ {
		a := 2 * math.Pi * float64(k) / 64
		loop = append(loop, pixelPoint{c + r*math.Cos(a), c + r*math.Sin(a)})
	}
	got := decodeFogTile(t, EncodeFogTile(renderActivityMask(loop), false))
	var outer, holes int
	for _, ring := range got.veil {
		if signedArea(ring) > 0 {
			outer++
		} else {
			holes++
		}
	}
	if outer != 2 || holes != 1 {
		t.Errorf("%d outer rings and %d holes, want the frame and the island, and the loop's hole", outer, holes)
	}
	if !inside(pt{c, c}, got.veil[len(got.veil)-1]) {
		t.Error("the island's ring doesn't hold the loop's centre")
	}
	if len(got.edges) != 2 {
		t.Errorf("%d edges, want the loop's outside and inside", len(got.edges))
	}
}

// A road crossing from one tile into the next meets itself at the shared edge: each tile traces
// its own pixels, carried on past its side, and the two edges reach it at the same place.
func TestFogTileEdgesMeetAcrossTiles(t *testing.T) {
	// A diagonal through the shared edge x = 128 of a left and a right tile.
	left := renderActivityMask([]pixelPoint{{40, 20}, {TileSize + 40, 100}})
	right := renderActivityMask([]pixelPoint{{40 - TileSize, 20}, {40, 100}})
	crossing := func(edges [][]pt, x float64) []float64 {
		var ys []float64
		for _, e := range edges {
			for i := 0; i+1 < len(e); i++ {
				a, b := e[i], e[i+1]
				if (a.x-x)*(b.x-x) <= 0 && a.x != b.x {
					ys = append(ys, a.y+(x-a.x)/(b.x-a.x)*(b.y-a.y))
				}
			}
		}
		return ys
	}
	l := crossing(decodeFogTile(t, EncodeFogTile(left, false)).edges, TileSize)
	r := crossing(decodeFogTile(t, EncodeFogTile(right, false)).edges, 0)
	if len(l) != 2 || len(r) != 2 {
		t.Fatalf("crossings %v and %v, want both edges of the road on each side", l, r)
	}
	if l[0] > l[1] {
		l[0], l[1] = l[1], l[0]
	}
	if r[0] > r[1] {
		r[0], r[1] = r[1], r[0]
	}
	for i := range 2 {
		if d := math.Abs(l[i] - r[i]); d > 0.35 {
			t.Errorf("edge %d meets the shared side at y %.2f on the left, %.2f on the right: %.2f px apart", i, l[i], r[i], d)
		}
	}
}

// A pooled tile traces a diagonal road as the line it is, not the staircase of whole pixels its
// coverage steps along: the edge strays from a straight line by less than half a pixel.
func TestFogTilePooledDiagonal(t *testing.T) {
	// A shallow diagonal over 8x8 z14 tiles, pooled three times into one z11 tile.
	level := make([][]*image.Gray, 8)
	for ty := range 8 {
		level[ty] = make([]*image.Gray, 8)
		for tx := range 8 {
			ox, oy := float64(tx*TileSize), float64(ty*TileSize)
			level[ty][tx] = renderActivityMask([]pixelPoint{{30 - ox, 100 - oy}, {1000 - ox, 400 - oy}})
		}
	}
	for n := 8; n > 1; n /= 2 {
		next := make([][]*image.Gray, n/2)
		for y := range n / 2 {
			next[y] = make([]*image.Gray, n/2)
			for x := range n / 2 {
				next[y][x] = downsampleQuadrants([4]*image.Gray{level[2*y][2*x], level[2*y][2*x+1], level[2*y+1][2*x], level[2*y+1][2*x+1]})
			}
		}
		level = next
	}
	cov := level[0][0]
	for _, c := range []struct {
		pooled bool
		within float64
		why    string
	}{{true, 0.5, "smoothed"}, {false, 10, "as traced"}} {
		var worst float64
		// Each side of the road away from its start, against the straight line between that
		// side's ends over the same stretch.
		a, b := pt{30.0 / 8, 100.0 / 8}, pt{1000.0 / 8, 400.0 / 8} // the road, in z11 pixels
		sides := map[bool][]pt{}
		for _, e := range decodeFogTile(t, EncodeFogTile(cov, c.pooled)).edges {
			for _, p := range e {
				if p.x > 20 && p.x < 110 {
					above := (b.x-a.x)*(p.y-a.y)-(b.y-a.y)*(p.x-a.x) < 0
					sides[above] = append(sides[above], p)
				}
			}
		}
		if len(sides) != 2 {
			t.Fatalf("%s: edge points on %d sides of the road, want both", c.why, len(sides))
		}
		for _, side := range sides {
			sort.Slice(side, func(i, j int) bool { return side[i].x < side[j].x })
			for _, p := range side {
				worst = math.Max(worst, segDist(p, side[0], side[len(side)-1]))
			}
		}
		if worst > c.within {
			t.Errorf("%s: edge strays %.2f px from straight, want under %.2f", c.why, worst, c.within)
		}
		if !c.pooled && worst < 0.5 {
			t.Errorf("as traced: edge strays only %.2f px; the test's tile no longer steps", worst)
		}
	}
}

func TestClipPolyline(t *testing.T) {
	line := []pt{{-5, 10}, {10, 10}, {10, 200}, {20, 200}, {20, 10}}
	got := clipPolyline(line, 0, 128)
	if len(got) != 2 {
		t.Fatalf("pieces %v, want two: in, out past the bottom, back in", got)
	}
	near := func(a, b pt) bool { return math.Hypot(a.x-b.x, a.y-b.y) < 1e-9 }
	if !near(got[0][0], pt{0, 10}) || !near(got[0][len(got[0])-1], pt{10, 128}) {
		t.Errorf("first piece %v, want (0,10) to (10,128)", got[0])
	}
	if !near(got[1][0], pt{20, 128}) || !near(got[1][len(got[1])-1], pt{20, 10}) {
		t.Errorf("second piece %v, want (20,128) to (20,10)", got[1])
	}
}

func TestSimplifyRing(t *testing.T) {
	// A square inside the tile with a point every unit along its sides: only the corners matter.
	var sq []pt
	for i := range 10 {
		sq = append(sq, pt{50 + float64(i), 50})
	}
	for i := range 10 {
		sq = append(sq, pt{60, 50 + float64(i)})
	}
	for i := range 10 {
		sq = append(sq, pt{60 - float64(i), 60})
	}
	for i := range 10 {
		sq = append(sq, pt{50, 60 - float64(i)})
	}
	got := simplifyRing(sq, 0.25)
	if len(got) != 4 || math.Abs(signedArea(got)) != 100 {
		t.Errorf("simplified %v, want the four corners", got)
	}
	// Along the tile's side every point stays: the next tile's outline meets them.
	side := []pt{{0.5, 10}, {0.5, 11}, {0.5, 12}, {5, 12}, {5, 10}}
	if got := simplifyRing(side, 0.25); len(got) != len(side) {
		t.Errorf("by the tile's side kept %v, want every point", got)
	}
}

func BenchmarkEncodeFogTile(b *testing.B) {
	m := image.NewGray(image.Rect(0, 0, TileSize, TileSize))
	for _, s := range []*image.Gray{benchStroke(), lineMask(30), lineMask(90)} {
		for i, v := range s.Pix {
			m.Pix[i] = max(m.Pix[i], v)
		}
	}
	for b.Loop() {
		EncodeFogTile(m, false)
	}
}
