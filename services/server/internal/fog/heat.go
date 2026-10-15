package fog

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
)

// Heatmap's stored tiles are pass counts (IMPLEMENTATION.md §4.2.2): for each pixel, how many
// in-window activities' strokes cover it, narrowed by a pixel first. They aren't coloured or
// blurred here — the clients draw them as MapLibre heatmap circles, from the points
// HeatPoints makes of them per request — so a change to how the heat looks never needs a
// re-render, and neither does a change of the account's heatmap_cap.

// HeatPointsMaxZoom is the last zoom heat-points tiles are made for: the one whose cells are a
// z14 count tile's own pixels. The clients enlarge it past there.
const HeatPointsMaxZoom = Zoom + 1

// heatCellsPerSide is how many cells a heat-points tile has across: a quarter of the count
// tile one zoom up, so 8 px each as the clients draw a 512 px tile — about one circle each.
const heatCellsPerSide = TileSize / 2

// HeatActivityMaxZoom is the last zoom whose cells are a z14 tile or bigger (a tile at zoom z has
// cells the size of zoom z+6 tiles). Up to it a cell is scored by how many activities pass
// through it — how often you're in that area; past it, by its busiest spot — how often you went
// down that street. Distinct activities in a cell finer than a z14 tile aren't on file: the
// counts are per pixel, and a pixel's counts can't be added up into a bigger area's activities.
const HeatActivityMaxZoom = Zoom - 6

// heatRadiusCells is the clients' circle radius in cells (apps/web/src/map/heatPoints.ts'
// RADIUS_PER_CELL, Android's heatPointsRadiusStops) — what weighNeighbours undoes the overlap of.
const heatRadiusCells = 1.6

// heatCounts counts, per pixel, the activities whose stroke covers it in one z14 tile. Each
// mask is narrowed by a pixel (erodeTile) before it counts: the ~100 m stroke Fog clears is
// wider than a route reads as heat. Counted per activity, so one activity through a pixel twice
// still counts once there, and an activity idling in place can't heat it.
func heatCounts(masks []*image.Gray) *image.Gray16 {
	counts := make([]uint16, TileSize*TileSize)
	narrowed := make([]uint8, TileSize*TileSize)
	tmp := make([]uint8, TileSize*TileSize)
	for _, m := range masks {
		erodeTile(narrowed, tmp, m.Pix)
		for i, px := range narrowed {
			if px > 127 && counts[i] < math.MaxUint16 {
				counts[i]++
			}
		}
	}
	out := image.NewGray16(image.Rect(0, 0, TileSize, TileSize))
	for i, v := range counts {
		out.Pix[2*i], out.Pix[2*i+1] = uint8(v>>8), uint8(v)
	}
	return out
}

// erodeTile narrows a TileSize mask's stroke by a pixel on each side: each pixel becomes the
// darkest of the 3×3 block around it — a row's 3, then a column's 3, through tmp. Past the
// tile's edge it repeats the edge pixel rather than counting as dark, so a track crossing into
// the next tile isn't cut short at the edge.
func erodeTile(dst, tmp, src []uint8) {
	const n = TileSize
	for y := range n {
		row, out := src[y*n:(y+1)*n], tmp[y*n:(y+1)*n]
		out[0] = min(row[0], row[1])
		for x := 1; x < n-1; x++ {
			out[x] = min(row[x-1], row[x], row[x+1])
		}
		out[n-1] = min(row[n-2], row[n-1])
	}
	for y := range n {
		up, mid, down := tmp[max(y-1, 0)*n:], tmp[y*n:], tmp[min(y+1, n-1)*n:]
		out := dst[y*n : (y+1)*n]
		for x := range n {
			out[x] = min(up[x], mid[x], down[x])
		}
	}
}

// downsampleCounts builds a parent count tile from its four (possibly blank) children: each
// pixel the most passes of its 2×2 block — the busiest spot in it, so a route passed once stays
// one pass at every zoom, however many pixels it crosses, rather than adding up.
func downsampleCounts(children [4]*image.Gray16) *image.Gray16 {
	out := image.NewGray16(image.Rect(0, 0, TileSize, TileSize))
	const half = TileSize / 2
	for q, child := range children {
		ox, oy := (q%2)*half, (q/2)*half
		for y := range half {
			r0 := child.Pix[(2*y)*child.Stride:]
			r1 := child.Pix[(2*y+1)*child.Stride:]
			dst := out.Pix[(oy+y)*out.Stride+2*ox:]
			for x := range half {
				v := max(px16(r0, 2*x), px16(r0, 2*x+1), px16(r1, 2*x), px16(r1, 2*x+1))
				dst[2*x], dst[2*x+1] = uint8(v>>8), uint8(v)
			}
		}
	}
	return out
}

// px16 is the x'th big-endian 16-bit pixel of a Gray16 row.
func px16(row []uint8, x int) uint16 { return uint16(row[2*x])<<8 | uint16(row[2*x+1]) }

// isBlankCounts reports whether a count tile has no passes in it.
func isBlankCounts(c *image.Gray16) bool {
	for _, b := range c.Pix {
		if b != 0 {
			return false
		}
	}
	return true
}

// blankCounts is a count tile with nothing in it: the stand-in for a missing child.
func blankCounts() *image.Gray16 {
	return image.NewGray16(image.Rect(0, 0, TileSize, TileSize))
}

// errNotCounts is decodeCounts' refusal of a stored Heatmap tile from before the counts: an
// 8-bit intensity raster, still on file until the deploy's rerender-coverage redraws it.
var errNotCounts = errors.New("fog: stored Heatmap tile is not a count tile")

// decodeCounts decodes a count tile this package wrote.
func decodeCounts(b []byte) (*image.Gray16, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if r := img.Bounds(); r.Dx() != TileSize || r.Dy() != TileSize {
		return nil, fmt.Errorf("%w: %dx%d, want %d", errTileSize, r.Dx(), r.Dy(), TileSize)
	}
	g, ok := img.(*image.Gray16)
	if !ok {
		return nil, errNotCounts
	}
	return g, nil
}

// HeatPoint is one cell of a heat-points tile: its centre in the tile's 4096-unit vector
// extent, and its weight, 0..1.
type HeatPoint struct {
	X, Y int
	W    float64
}

// HeatPoints makes the points of heat-points tile (z, x, y), past HeatActivityMaxZoom, from the
// count tile one zoom up (parent, HeatPointsSource): each cell's busiest pixel, weighted on a log scale
// against scale (HeatScale). Empty cells make no point.
func HeatPoints(parent *image.Gray16, x, y int, scale float64) []HeatPoint {
	const extent = 4096
	cell := extent / heatCellsPerSide
	logScale := math.Log1p(scale)
	// The tile's quarter of its parent.
	ox, oy := (x%2)*heatCellsPerSide, (y%2)*heatCellsPerSide
	var out []HeatPoint
	for cy := range heatCellsPerSide {
		row := parent.Pix[(oy+cy)*parent.Stride:]
		for cx := range heatCellsPerSide {
			n := px16(row, ox+cx)
			if n == 0 {
				continue
			}
			out = append(out, HeatPoint{
				X: cx*cell + cell/2,
				Y: cy*cell + cell/2,
				W: math.Min(1, math.Log1p(float64(n))/logScale),
			})
		}
	}
	return weighNeighbours(out)
}

// HeatCell is one zoomed-out cell's count of the activities through it: (CX, CY) at zoom z+6 for
// a heat-points tile at zoom z.
type HeatCell struct {
	CX, CY int
	N      int
}

// HeatCellPoints makes the points of heat-points tile (z, x, y), up to HeatActivityMaxZoom, from
// its cells' activity counts, weighted on a log scale against scale (HeatScale).
func HeatCellPoints(cells []HeatCell, x, y int, scale float64) []HeatPoint {
	const extent = 4096
	cell := extent / heatCellsPerSide
	logScale := math.Log1p(scale)
	out := make([]HeatPoint, 0, len(cells))
	for _, c := range cells {
		cx, cy := c.CX-x*heatCellsPerSide, c.CY-y*heatCellsPerSide
		if c.N <= 0 || cx < 0 || cy < 0 || cx >= heatCellsPerSide || cy >= heatCellsPerSide {
			continue
		}
		out = append(out, HeatPoint{
			X: cx*cell + cell/2,
			Y: cy*cell + cell/2,
			W: math.Min(1, math.Log1p(float64(c.N))/logScale),
		})
	}
	return weighNeighbours(out)
}

// HeatScale is what a zoom's weights are measured against: its busiest cell (zoomCap, from
// users.heat_point_caps; 0 when not stored), but never less than the account's heatmap_cap, so a
// sparse account's few-pass streets don't read as its busiest.
func HeatScale(zoomCap int, heatmapCap float64) float64 {
	return math.Max(math.Max(float64(zoomCap), heatmapCap), minHeatmapCap)
}

// weighNeighbours divides each point's weight by how much its neighbours' circles add at its
// centre. MapLibre adds overlapping circles, so without it a route — a line of points — read
// hotter than a lone place with the same count, and a busy area hotter still; with it, every
// point's circle peaks at its own weight (the clients' intensity undoes the kernel's own 0.4),
// whatever lies around it. The kernel is MapLibre's: exp(-½ (3d/r)²) at d cells for radius r.
// Neighbours in the next tile aren't seen, so a point on a tile's edge reads a touch hot.
func weighNeighbours(points []HeatPoint) []HeatPoint {
	const extent = 4096
	cell := extent / heatCellsPerSide
	at := make(map[[2]int]bool, len(points))
	for _, p := range points {
		at[[2]int{p.X / cell, p.Y / cell}] = true
	}
	for i, p := range points {
		cx, cy := p.X/cell, p.Y/cell
		overlap := 1.0
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				if (dx != 0 || dy != 0) && at[[2]int{cx + dx, cy + dy}] {
					d := 3 * math.Hypot(float64(dx), float64(dy)) / heatRadiusCells
					overlap += math.Exp(-0.5 * d * d)
				}
			}
		}
		points[i].W = p.W / overlap
	}
	return points
}

// HeatPointsSource is the count tile heat-points tile (z, x, y), past HeatActivityMaxZoom, is
// made from: zoom z-1's.
func HeatPointsSource(z, x, y int) (int, int, int) {
	return z - 1, x / 2, y / 2
}

// DecodeCounts is decodeCounts for the tile route: nil (no heat) for a tile not yet redrawn as
// counts, rather than an error.
func DecodeCounts(b []byte) (*image.Gray16, error) {
	if b == nil {
		return blankCounts(), nil
	}
	g, err := decodeCounts(b)
	if errors.Is(err, errNotCounts) {
		return blankCounts(), nil
	}
	return g, err
}
