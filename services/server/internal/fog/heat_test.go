package fog

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// lineMask is one activity's mask with a horizontal stroke at tile-local y, edge to edge.
func lineMask(y float64) *image.Gray {
	return renderActivityMask([]pixelPoint{{-10, y}, {TileSize + 10, y}})
}

// A pixel counts the activities whose narrowed stroke covers it: two activities on one line
// make 2 there, the stroke's narrowed edges nothing, and the line runs to the tile's edges.
func TestHeatCounts(t *testing.T) {
	const mid = TileSize / 2
	counts := heatCounts([]*image.Gray{lineMask(mid), lineMask(mid), lineMask(20)})
	if got := counts.Gray16At(mid, mid).Y; got != 2 {
		t.Errorf("on the shared line: %d, want 2", got)
	}
	if got := counts.Gray16At(mid, 20).Y; got != 1 {
		t.Errorf("on the lone line: %d, want 1", got)
	}
	// Fog's stroke reaches ~2.5 px either side; narrowed by a pixel, 2 px off is outside it.
	if got := counts.Gray16At(mid, mid+2).Y; got != 0 {
		t.Errorf("2 px off the line: %d, want 0 once narrowed", got)
	}
	for _, x := range []int{0, TileSize - 1} {
		if got := counts.Gray16At(x, mid).Y; got != 2 {
			t.Errorf("at the tile's edge (x %d): %d, want 2 — narrowing mustn't cut the line short there", x, got)
		}
	}
}

func TestErodeTileNarrowsByAPixel(t *testing.T) {
	src := make([]uint8, TileSize*TileSize)
	for y := 10; y <= 14; y++ { // a band 5 px tall
		for x := range TileSize {
			src[y*TileSize+x] = 255
		}
	}
	dst := make([]uint8, len(src))
	erodeTile(dst, make([]uint8, len(src)), src)
	for y := 8; y <= 16; y++ {
		want := uint8(0)
		if y >= 11 && y <= 13 {
			want = 255
		}
		if got := dst[y*TileSize+5]; got != want {
			t.Errorf("row %d: %d, want %d", y, got, want)
		}
	}
}

// A parent pixel is its 2×2 block's busiest child pixel, from the right child.
func TestDownsampleCounts(t *testing.T) {
	var c [4]*image.Gray16
	for q := range c {
		c[q] = blankCounts()
	}
	c[3].SetGray16(10, 20, color.Gray16{Y: 7})
	c[3].SetGray16(11, 21, color.Gray16{Y: 3})
	got := downsampleCounts(c)
	if v := got.Gray16At(TileSize/2+5, TileSize/2+10).Y; v != 7 {
		t.Errorf("bottom-right child's block: %d, want its busiest pixel, 7", v)
	}
	if v := got.Gray16At(5, 10).Y; v != 0 {
		t.Errorf("top-left child's block: %d, want 0", v)
	}
}

func TestCountsRoundTrip(t *testing.T) {
	c := blankCounts()
	c.SetGray16(1, 2, color.Gray16{Y: 600})
	b, err := encodeTilePNG(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeCounts(b)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Gray16At(1, 2).Y; v != 600 {
		t.Errorf("round trip: %d, want 600", v)
	}
}

// A Heatmap tile still in the 8-bit raster from before the counts reads as no heat, not as
// counts in the hundreds.
func TestDecodeCountsTreatsTheOldRasterAsBlank(t *testing.T) {
	old := blankTile()
	old.Pix[0] = 200
	b, err := encodeTilePNG(old)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCounts(b)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Gray16At(0, 0).Y; v != 0 {
		t.Errorf("old raster pixel: %d passes, want 0", v)
	}
	if got, err := DecodeCounts(nil); err != nil || got.Gray16At(0, 0).Y != 0 {
		t.Errorf("nothing stored: %v, %v; want a blank tile", got, err)
	}
}

// A heat-points tile is its quarter of the count tile one zoom up, a point per non-empty
// pixel at its centre, weighted on the log scale of the scale; lone points keep their weight.
func TestHeatPoints(t *testing.T) {
	parent := blankCounts()
	parent.SetGray16(TileSize/2+2, 4, color.Gray16{Y: 1}) // top-right quarter
	parent.SetGray16(TileSize/2+20, 30, color.Gray16{Y: 8})
	pts := HeatPoints(parent, 301, 200, 8) // x odd, y even: the top-right quarter
	if len(pts) != 2 {
		t.Fatalf("points %v, want 2", pts)
	}
	cell := 4096 / heatCellsPerSide
	if p := pts[0]; p.X != 2*cell+cell/2 || p.Y != 4*cell+cell/2 || math.Abs(p.W-math.Log1p(1)/math.Log1p(8)) > 1e-9 {
		t.Errorf("first point %+v, want cell (2, 4), one pass's weight", p)
	}
	if p := pts[1]; p.W != 1 {
		t.Errorf("a cell at the scale weighs %v, want 1", p.W)
	}
	if got := HeatPoints(parent, 300, 200, 8); len(got) != 0 {
		t.Errorf("the empty top-left quarter made %v", got)
	}
}

// Zoomed out, a point is a cell's activities, placed by its cell within the tile; cells outside
// the tile and empty ones make none.
func TestHeatCellPoints(t *testing.T) {
	const x, y = 3, 5
	cells := []HeatCell{
		{CX: x*heatCellsPerSide + 10, CY: y*heatCellsPerSide + 20, N: 14},
		{CX: x*heatCellsPerSide + 40, CY: y*heatCellsPerSide + 50, N: 2},
		{CX: (x+1)*heatCellsPerSide + 1, CY: y * heatCellsPerSide, N: 5}, // the next tile's
		{CX: x * heatCellsPerSide, CY: y * heatCellsPerSide, N: 0},
	}
	pts := HeatCellPoints(cells, x, y, 14)
	if len(pts) != 2 {
		t.Fatalf("points %v, want 2", pts)
	}
	cell := 4096 / heatCellsPerSide
	if p := pts[0]; p.X != 10*cell+cell/2 || p.Y != 20*cell+cell/2 || p.W != 1 {
		t.Errorf("busiest cell %+v, want cell (10, 20) at weight 1", p)
	}
	if p := pts[1]; math.Abs(p.W-math.Log1p(2)/math.Log1p(14)) > 1e-9 {
		t.Errorf("two activities weigh %v, want %v", p.W, math.Log1p(2)/math.Log1p(14))
	}
}

func TestHeatScale(t *testing.T) {
	for _, c := range []struct {
		zoomCap    int
		heatmapCap float64
		want       float64
	}{
		{29, 8, 29},           // a zoomed-out cell busier than any one tile
		{4, 8, 8},             // never below heatmap_cap
		{0, 114, 114},         // none stored yet
		{1, 1, minHeatmapCap}, // never below the floor
	} {
		if got := HeatScale(c.zoomCap, c.heatmapCap); got != c.want {
			t.Errorf("HeatScale(%d, %v) = %v, want %v", c.zoomCap, c.heatmapCap, got, c.want)
		}
	}
}

// A point's weight is divided by its neighbours' overlap: alone it keeps its weight, along a
// line it loses about a third, in a filled area most.
func TestWeighNeighbours(t *testing.T) {
	cell := 4096 / heatCellsPerSide
	at := func(cx, cy int) HeatPoint { return HeatPoint{X: cx*cell + cell/2, Y: cy*cell + cell/2, W: 1} }
	lone := weighNeighbours([]HeatPoint{at(10, 10)})
	if lone[0].W != 1 {
		t.Errorf("a lone point weighs %v, want 1", lone[0].W)
	}
	line := weighNeighbours([]HeatPoint{at(9, 10), at(10, 10), at(11, 10)})
	area := make([]HeatPoint, 0, 25)
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			area = append(area, at(10+dx, 10+dy))
		}
	}
	area = weighNeighbours(area)
	mid := area[12].W
	if w := line[1].W; !(w < 0.8 && w > 0.7) {
		t.Errorf("the middle of a line weighs %v, want about 0.74", w)
	}
	if !(mid < line[1].W) {
		t.Errorf("the middle of an area weighs %v, want less than a line's %v", mid, line[1].W)
	}
}

func TestHeatPointsSource(t *testing.T) {
	for _, c := range []struct{ z, x, y, wz, wx, wy int }{
		{9, 279, 382, 8, 139, 191},
		{15, 8943, 12239, 14, 4471, 6119},
	} {
		if z, x, y := HeatPointsSource(c.z, c.x, c.y); z != c.wz || x != c.wx || y != c.wy {
			t.Errorf("tile %d/%d/%d: source %d/%d/%d, want %d/%d/%d", c.z, c.x, c.y, z, x, y, c.wz, c.wx, c.wy)
		}
	}
}
