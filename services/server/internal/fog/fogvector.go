package fog

import (
	"bytes"
	"encoding/binary"
	"image"
	"math"
	"sync"
)

// Fog as vector shapes (IMPLEMENTATION.md §4.2): the veil is traced from a stored coverage tile
// when a tile is asked for, as polygons and the lines along their edges, so the clients draw it
// crisp at every zoom — a 128 px raster drawn 512 px wide was either square pixels or blur —
// and soften only its edge, with a blurred line. Nothing new is stored: the outline is traced
// from the PNG the render already keeps.

// fogLevel is the coverage the outline is traced at: half way between fogged and cleared, where
// the anti-aliased stroke's edge is.
const fogLevel = 127.5

// fogExtent is the vector tile's extent: 32 units to a coverage pixel.
const fogExtent = 4096

// fogSimplify is how far, in coverage pixels, a traced outline may move to drop a point: a
// quarter of a pixel, ~5 m at z14, below anything drawn.
const fogSimplify = 0.25

// pt is a point in coverage pixels: (0, 0) the tile's top left corner, (TileSize, TileSize) its
// bottom right.
type pt struct{ x, y float64 }

// fogPooledSmooth is how far along a pooled tile's outline, in coverage pixels (one standard
// deviation), its points are averaged. Below z14 a tile is its children's brightest pixels
// (downsampleQuadrants), so a corridor's side runs along whole pixels: traced as it is, a
// diagonal road is a staircase. Averaged over a pixel or so either way, the stairs become the
// line they step along; a z14 tile, its strokes anti-aliased, already traces smooth.
const fogPooledSmooth = 1.0

// EncodeFogTile traces a coverage tile and encodes it as a vector tile: one layer, "fog", with a
// polygon feature for the veil (kind "veil": everywhere not cleared) and a line feature along
// every edge between cleared and veiled ground (kind "edge"), for the clients to draw blurred.
// pooled says the tile is a pyramid level above z14 (fogPooledSmooth).
func EncodeFogTile(cov *image.Gray, pooled bool) []byte {
	sc := fogScratchPool.Get().(*fogScratch)
	defer fogScratchPool.Put(sc)
	coverageField(sc.field, cov)
	smooth := 0.0
	if pooled {
		smooth = fogPooledSmooth
	}
	rings := traceRings(sc, smooth)
	veil := veilPolygons(rings)
	var edges [][]pt
	for _, r := range rings {
		edges = append(edges, clipPolyline(append(r.pts, r.pts[0]), -0.5, TileSize+0.5)...)
	}
	return encodeFogMVT(veil, edges)
}

// fogN is the side of the sample grid: the tile, a ring past its edge, and an outer ring.
const fogN = TileSize + 4

// fogScratch is one trace's working memory, kept between requests (fogScratchPool): allocated
// afresh, the grid and its edges were most of a tile's cost, as garbage.
type fogScratch struct {
	field []float32  // fogN², row by row
	links [][2]int32 // per grid edge
	seen  []bool     // per grid edge
}

var fogScratchPool = sync.Pool{New: func() any {
	return &fogScratch{
		field: make([]float32, fogN*fogN),
		links: make([][2]int32, fogN*fogN*2),
		seen:  make([]bool, fogN*fogN*2),
	}
}}

// coverageField fills f with the coverage as a fogN² grid of samples at pixel centres: the
// tile's own pixels, a ring past its edge carrying each row and column on in a straight line —
// so an outline crossing the edge carries on as it was going, and meets the next tile's own
// close to where it reaches the edge — and an outer ring of nothing, so every traced outline
// closes. Sample (i, j) sits at pixel (j-1.5, i-1.5).
func coverageField(f []float32, cov *image.Gray) {
	const n = fogN
	clear(f)
	for y := range TileSize {
		for x := range TileSize {
			f[(y+2)*n+x+2] = float32(cov.Pix[y*cov.Stride+x])
		}
	}
	clamp := func(v float32) float32 { return min(max(v, 0), 255) }
	for i := 2; i < n-2; i++ {
		f[i*n+1] = clamp(2*f[i*n+2] - f[i*n+3])
		f[i*n+n-2] = clamp(2*f[i*n+n-3] - f[i*n+n-4])
	}
	for j := 1; j < n-1; j++ {
		f[n+j] = clamp(2*f[2*n+j] - f[3*n+j])
		f[(n-2)*n+j] = clamp(2*f[(n-3)*n+j] - f[(n-4)*n+j])
	}
}

// ring is one closed outline at fogLevel, in coverage pixels, with how deeply other outlines
// enclose it: at an even depth it encloses cleared ground, at an odd one a veiled island
// inside cleared ground.
type ring struct {
	pts    []pt
	area   float64 // signed, y down: positive is clockwise on screen
	depth  int
	parent int // the smallest ring enclosing it, -1 for none
}

// traceRings traces every outline at fogLevel through the field by marching squares, each cell's
// crossings joined into closed rings, smoothed by smooth (smoothRing), simplified, and nested. A
// grid edge is numbered (i*n+j)*2, plus one if it runs down from sample (i, j) rather than right.
func traceRings(sc *fogScratch, smooth float64) []ring {
	const n = fogN
	f := sc.field
	in := func(i, j int) bool { return f[i*n+j] > fogLevel }
	at := func(e int) pt {
		i, j, down := e/2/n, e/2%n, e%2 == 1
		a := float64(f[i*n+j])
		b := float64(f[i*n+j+1])
		if down {
			b = float64(f[(i+1)*n+j])
		}
		t := (fogLevel - a) / (b - a)
		x, y := float64(j)-1.5, float64(i)-1.5
		if down {
			return pt{x, y + t}
		}
		return pt{x + t, y}
	}
	// Every crossed edge joins exactly two others; -1 is none yet.
	links := sc.links
	for e := range links {
		links[e] = [2]int32{-1, -1}
	}
	link := func(a, b int) {
		for _, p := range [2][2]int{{a, b}, {b, a}} {
			if links[p[0]][0] < 0 {
				links[p[0]][0] = int32(p[1])
			} else {
				links[p[0]][1] = int32(p[1])
			}
		}
	}
	var crossed [4]int
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-1; j++ {
			tl, tr, br, bl := in(i, j), in(i, j+1), in(i+1, j+1), in(i+1, j)
			if tl == tr && tr == br && br == bl {
				continue
			}
			top, right, bottom, left := (i*n+j)*2, (i*n+j+1)*2+1, ((i+1)*n+j)*2, (i*n+j)*2+1
			k := 0
			for _, c := range [4]struct {
				cross bool
				e     int
			}{{tl != tr, top}, {tr != br, right}, {br != bl, bottom}, {bl != tl, left}} {
				if c.cross {
					crossed[k] = c.e
					k++
				}
			}
			if k == 2 {
				link(crossed[0], crossed[1])
				continue
			}
			// A saddle: the cell's centre decides which pair of corners connects. A corner cut
			// off on its own is cut off with its two edges.
			centreIn := (f[i*n+j]+f[i*n+j+1]+f[(i+1)*n+j+1]+f[(i+1)*n+j])/4 > fogLevel
			if tl != centreIn { // top left and bottom right are cut off
				link(top, left)
				link(right, bottom)
			} else { // top right and bottom left are
				link(top, right)
				link(bottom, left)
			}
		}
	}
	seen := sc.seen
	clear(seen)
	var rings []ring
	for start := range links {
		if links[start][0] < 0 || seen[start] {
			continue
		}
		var pts []pt
		prev, cur := -1, start
		for {
			seen[cur] = true
			pts = append(pts, at(cur))
			next := int(links[cur][0])
			if next == prev {
				next = int(links[cur][1])
			}
			prev, cur = cur, next
			if cur == start {
				break
			}
		}
		if smooth > 0 {
			pts = smoothRing(pts, smooth)
		}
		pts = simplifyRing(pts, fogSimplify)
		if len(pts) < 3 {
			continue
		}
		rings = append(rings, ring{pts: pts, area: signedArea(pts), parent: -1})
	}
	nestRings(rings)
	return rings
}

// nestRings sets each ring's parent, the smallest ring around it, and its depth. Outlines never
// cross, so one point of a ring tells which rings it is inside.
func nestRings(rings []ring) {
	for a := range rings {
		best := -1
		for b := range rings {
			if a == b || math.Abs(rings[b].area) <= math.Abs(rings[a].area) {
				continue
			}
			if inside(rings[a].pts[0], rings[b].pts) && (best < 0 || math.Abs(rings[b].area) < math.Abs(rings[best].area)) {
				best = b
			}
		}
		rings[a].parent = best
	}
	for a := range rings {
		for p := rings[a].parent; p >= 0; p = rings[p].parent {
			rings[a].depth++
		}
	}
}

// veilPolygons is the veil as polygons, each an outer ring then its holes, wound as vector
// tiles want them (outer clockwise on screen, holes counter-clockwise): a frame just past the
// tile with every outermost cleared area cut out of it, and each veiled island inside cleared
// ground with the cleared areas inside it cut out in turn.
func veilPolygons(rings []ring) [][][]pt {
	const lo, hi = -2.0, TileSize + 2.0
	frame := [][]pt{{{lo, lo}, {hi, lo}, {hi, hi}, {lo, hi}}}
	var polys [][][]pt
	islands := map[int]int{} // ring -> its polygon
	for i, r := range rings {
		if r.depth%2 == 1 {
			islands[i] = len(polys)
			polys = append(polys, [][]pt{wind(r.pts, true)})
		}
	}
	for _, r := range rings {
		if r.depth%2 == 0 {
			if r.parent < 0 {
				frame = append(frame, wind(r.pts, false))
			} else {
				p := islands[r.parent]
				polys[p] = append(polys[p], wind(r.pts, false))
			}
		}
	}
	return append([][][]pt{frame}, polys...)
}

// wind returns pts clockwise on screen (y down) if outer, else counter-clockwise.
func wind(pts []pt, outer bool) []pt {
	if (signedArea(pts) > 0) == outer {
		return pts
	}
	out := make([]pt, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// signedArea is a ring's area by the surveyor's formula, y down: positive when clockwise on
// screen.
func signedArea(pts []pt) float64 {
	a := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		a += p.x*q.y - q.x*p.y
	}
	return a / 2
}

// inside reports whether p is inside the ring, by the even-odd rule.
func inside(p pt, ring []pt) bool {
	in := false
	for i, a := range ring {
		b := ring[(i+1)%len(ring)]
		if (a.y > p.y) != (b.y > p.y) && p.x < a.x+(p.y-a.y)/(b.y-a.y)*(b.x-a.x) {
			in = !in
		}
	}
	return in
}

// nearSide reports whether p is within a pixel of the tile's side, where the next tile's outline
// has to meet this one's: smoothing and simplifying leave such a point where it is.
func nearSide(p pt) bool {
	return p.x < 1 || p.y < 1 || p.x > TileSize-1 || p.y > TileSize-1
}

// smoothRing moves each point of a closed ring to the average of the points around it, weighed
// by a Gaussian of their distance along the ring with standard deviation sigma pixels.
func smoothRing(pts []pt, sigma float64) []pt {
	n := len(pts)
	if n < 4 {
		return pts
	}
	step := make([]float64, n) // from point i to i+1
	for i, p := range pts {
		q := pts[(i+1)%n]
		step[i] = math.Hypot(q.x-p.x, q.y-p.y)
	}
	reach := 3 * sigma
	out := make([]pt, n)
	for i, p := range pts {
		if nearSide(p) {
			out[i] = p
			continue
		}
		sx, sy, sw := p.x, p.y, 1.0
		// Out along the ring each way, until the points are reach away or it comes back round.
		for _, dir := range [2]int{1, -1} {
			d := 0.0
			for k := 1; k < n/2; k++ {
				j := ((i+dir*k)%n + n) % n
				if dir > 0 {
					d += step[(j-1+n)%n]
				} else {
					d += step[j]
				}
				if d > reach {
					break
				}
				w := math.Exp(-d * d / (2 * sigma * sigma))
				sx += w * pts[j].x
				sy += w * pts[j].y
				sw += w
			}
		}
		out[i] = pt{sx / sw, sy / sw}
	}
	return out
}

// simplifyRing drops points of a closed ring in one pass: from the last point kept, a run of
// points goes while every point in it lies within tol of the line to the run's end — a
// marching-squares outline has a point on every grid edge it crosses, most of them on straight
// runs. A run is at most simplifyRun points, which bounds the cost; Douglas-Peucker's worst case
// is quadratic, and long smooth outlines come close to it. A point by the tile's side (nearSide)
// is always kept.
func simplifyRing(pts []pt, tol float64) []pt {
	if len(pts) < 4 {
		return pts
	}
	const simplifyRun = 48
	out := []pt{pts[0]}
	anchor := 0
	for i := 1; i < len(pts); i++ {
		end := pts[(i+1)%len(pts)]
		ok := i-anchor < simplifyRun && !nearSide(pts[i])
		for k := anchor + 1; ok && k <= i; k++ {
			ok = segDist(pts[k], pts[anchor], end) <= tol
		}
		if !ok {
			out = append(out, pts[i])
			anchor = i
		}
	}
	return out
}

// segDist is p's distance from the segment a–b.
func segDist(p, a, b pt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	t := math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	return math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy)
}

// clipPolyline cuts a line to the square [lo, hi]², returning the pieces inside it. It keeps an
// edge to where the coverage carries on past the tile, and drops the part of an outline that
// only closes it around the field's border.
func clipPolyline(line []pt, lo, hi float64) [][]pt {
	var pieces [][]pt
	var cur []pt
	for i := 0; i+1 < len(line); i++ {
		a, b, ok := clipSegment(line[i], line[i+1], lo, hi)
		if !ok {
			if len(cur) > 1 {
				pieces = append(pieces, cur)
			}
			cur = nil
			continue
		}
		if len(cur) == 0 || cur[len(cur)-1] != a {
			if len(cur) > 1 {
				pieces = append(pieces, cur)
			}
			cur = []pt{a}
		}
		cur = append(cur, b)
	}
	if len(cur) > 1 {
		pieces = append(pieces, cur)
	}
	return pieces
}

// clipSegment clips a–b to the square [lo, hi]² (Liang-Barsky).
func clipSegment(a, b pt, lo, hi float64) (pt, pt, bool) {
	t0, t1 := 0.0, 1.0
	dx, dy := b.x-a.x, b.y-a.y
	for _, c := range [4][2]float64{{-dx, a.x - lo}, {dx, hi - a.x}, {-dy, a.y - lo}, {dy, hi - a.y}} {
		p, q := c[0], c[1]
		if p == 0 {
			if q < 0 {
				return a, b, false
			}
			continue
		}
		r := q / p
		if p < 0 {
			t0 = math.Max(t0, r)
		} else {
			t1 = math.Min(t1, r)
		}
		if t0 > t1 {
			return a, b, false
		}
	}
	return pt{a.x + t0*dx, a.y + t0*dy}, pt{a.x + t1*dx, a.y + t1*dy}, true
}

// encodeFogMVT encodes the veil polygons and edge lines as the "fog" layer of a vector tile.
func encodeFogMVT(veil [][][]pt, edges [][]pt) []byte {
	const scale = fogExtent / TileSize
	var layer []byte
	layer = appendVarintField(layer, 15, 2) // version
	layer = appendBytesField(layer, 1, []byte("fog"))

	var g geomWriter
	for _, poly := range veil {
		for _, r := range poly {
			g.ring(r, scale)
		}
	}
	if len(g.cmds) > 0 {
		layer = appendBytesField(layer, 2, fogFeature(0, 3, g.cmds)) // kind veil, POLYGON
	}
	g = geomWriter{}
	for _, l := range edges {
		g.line(l, scale)
	}
	if len(g.cmds) > 0 {
		layer = appendBytesField(layer, 2, fogFeature(1, 2, g.cmds)) // kind edge, LINESTRING
	}
	layer = appendBytesField(layer, 3, []byte("kind"))
	for _, v := range []string{"veil", "edge"} {
		layer = appendBytesField(layer, 4, appendBytesField(nil, 1, []byte(v))) // string value
	}
	layer = appendVarintField(layer, 5, fogExtent)
	return appendBytesField(nil, 3, layer)
}

func fogFeature(value, geomType uint64, cmds []uint64) []byte {
	var f, packed []byte
	packed = binary.AppendUvarint(packed, 0)
	packed = binary.AppendUvarint(packed, value)
	f = appendBytesField(f, 2, packed)
	f = appendVarintField(f, 3, geomType)
	packed = packed[:0]
	for _, c := range cmds {
		packed = binary.AppendUvarint(packed, c)
	}
	return appendBytesField(f, 4, packed)
}

// geomWriter builds a feature's geometry commands, its cursor carried from one part to the next
// as the format has it.
type geomWriter struct {
	cmds   []uint64
	cx, cy int
}

func (g *geomWriter) points(pts []pt, scale float64) [][2]int {
	out := make([][2]int, 0, len(pts))
	for _, p := range pts {
		q := [2]int{int(math.Round(p.x * scale)), int(math.Round(p.y * scale))}
		if len(out) == 0 || out[len(out)-1] != q {
			out = append(out, q)
		}
	}
	return out
}

func (g *geomWriter) emit(cmd, count int, pts [][2]int) {
	g.cmds = append(g.cmds, uint64(cmd|count<<3))
	for _, p := range pts {
		g.cmds = append(g.cmds, zigzag(p[0]-g.cx), zigzag(p[1]-g.cy))
		g.cx, g.cy = p[0], p[1]
	}
}

// ring writes a closed ring: MoveTo, LineTo the rest, ClosePath.
func (g *geomWriter) ring(pts []pt, scale float64) {
	q := g.points(pts, scale)
	if len(q) > 1 && q[0] == q[len(q)-1] {
		q = q[:len(q)-1]
	}
	if len(q) < 3 {
		return
	}
	g.emit(1, 1, q[:1])
	g.emit(2, len(q)-1, q[1:])
	g.cmds = append(g.cmds, 7|1<<3)
}

// line writes an open line: MoveTo, LineTo the rest.
func (g *geomWriter) line(pts []pt, scale float64) {
	q := g.points(pts, scale)
	if len(q) < 2 {
		return
	}
	g.emit(1, 1, q[:1])
	g.emit(2, len(q)-1, q[1:])
}

// DecodeCoverage decodes a stored Fog tile for EncodeFogTile: nil (no tile on file) is all
// veil, as FogTile serves it.
func DecodeCoverage(b []byte) (*image.Gray, error) {
	if b == nil {
		return blankTile(), nil
	}
	return decodeGray(bytes.NewReader(b))
}
