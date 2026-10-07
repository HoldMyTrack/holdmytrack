// Package tilemath is the slippy-map tile arithmetic shared by ingest (marking z14 fog
// tiles dirty from a raw trajectory) and fog (rendering a tile's bounds and its pyramid
// parent/child relationships) — the same standard Web Mercator scheme the basemap, tracks,
// and fog tiles all already share, kept in one place rather than duplicated per package.
package tilemath

import "math"

// LonLatToTile converts a WGS84 point to its slippy-map tile coordinate at the given zoom.
func LonLatToTile(lon, lat float64, zoom int) (x, y int) {
	n := math.Pow(2, float64(zoom))
	x = int(math.Floor((lon + 180) / 360 * n))
	latRad := lat * math.Pi / 180
	y = int(math.Floor((1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n))
	// Clamp rather than let a pole-adjacent or antimeridian point produce an out-of-range
	// tile index — GPS noise near ±90° latitude is the realistic trigger, not a real route.
	max := int(n) - 1
	if x < 0 {
		x = 0
	} else if x > max {
		x = max
	}
	if y < 0 {
		y = 0
	} else if y > max {
		y = max
	}
	return x, y
}

// WorldPixel projects a WGS84 point to its Web Mercator pixel coordinate at the given zoom
// and tile size — full floating-point precision, unlike LonLatToTile's floored tile index.
// Subtracting a tile's own origin (tileX*tileSize, tileY*tileSize) from this gives the
// tile-local pixel coordinate the rasterizer draws at. Linear lat/lon interpolation within
// a tile would be wrong here — Mercator Y is nonlinear in latitude — so this recomputes the
// same projection LonLatToTile uses, just scaled to sub-pixel precision instead of floored.
func WorldPixel(lon, lat float64, zoom int, tileSize float64) (x, y float64) {
	n := math.Pow(2, float64(zoom))
	x = (lon + 180) / 360 * n * tileSize
	latRad := lat * math.Pi / 180
	y = (1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n * tileSize
	return x, y
}

// TileBounds returns a tile's WGS84 envelope (minLon, minLat, maxLon, maxLat).
func TileBounds(x, y, zoom int) (minLon, minLat, maxLon, maxLat float64) {
	n := math.Pow(2, float64(zoom))
	minLon = float64(x)/n*360 - 180
	maxLon = float64(x+1)/n*360 - 180
	maxLat = tileYToLat(float64(y), n)
	minLat = tileYToLat(float64(y+1), n)
	return
}

func tileYToLat(y, n float64) float64 {
	rad := math.Atan(math.Sinh(math.Pi * (1 - 2*y/n)))
	return rad * 180 / math.Pi
}

// SegmentTiles returns every tile at the given zoom a straight segment between two points
// passes through, walking tile space rather than just the two endpoints' tiles —
// consecutive GPS fixes are usually within one tile at 1 Hz, but a fast segment (a car
// commute logged by mistake, a GPS glitch) or a sparse source (a Google Maps Timeline drive,
// a point every few minutes) can span many, and a gap here would leave a strip of
// genuinely-covered ground stuck fogged.
func SegmentTiles(lon1, lat1, lon2, lat2 float64, zoom int) [][2]int {
	return SegmentTilesBuffered(lon1, lat1, lon2, lat2, zoom, 0, 256)
}

// SegmentTilesBuffered is SegmentTiles plus every tile the segment comes within marginPx of
// (tile-local pixels, at tileSize) — the tile a bare coordinate floors into isn't the only
// tile the *rendered* stroke can touch, since fog/heatmap draws a track as a
// strokeRadiusPx-wide, then featherPx-blurred line, not an infinitesimal one. A segment that
// runs within marginPx of a tile edge has part of that drawn line geometrically inside the
// neighboring tile even though the segment itself never crosses over — without this, that
// neighbor is never rendered for the activity and gg's rasterizer silently clips the overflow
// at the canvas edge instead.
//
// It works on the segment's exact pixel coordinates, one tile column at a time: the part of
// the segment inside a column (widened by the margin) spans a range of rows, and every tile in
// that range is in. Walking the endpoints' tile indices instead (a Bresenham line between
// them) skips tiles a long, slanted segment cuts across near a corner. The margin is square,
// so a tile only the stroke's round end would reach diagonally is included too: an empty mask,
// never a missing one.
//
// A longitude past ±180 (a track continuing across the antimeridian, ingest's unwrapLons) is
// a column past the world's edge, which wraps round to the other side's.
func SegmentTilesBuffered(lon1, lat1, lon2, lat2 float64, zoom int, marginPx, tileSize float64) [][2]int {
	x1, y1 := WorldPixel(lon1, lat1, zoom, tileSize)
	x2, y2 := WorldPixel(lon2, lat2, zoom, tileSize)
	n := int(math.Pow(2, float64(zoom)))
	last := n - 1
	clamp := func(v int) int { return min(max(v, 0), last) }
	wrap := func(v int) int { return ((v % n) + n) % n }

	var out [][2]int
	lo := int(math.Floor((math.Min(x1, x2) - marginPx) / tileSize))
	hi := int(math.Floor((math.Max(x1, x2) + marginPx) / tileSize))
	for tx := lo; tx <= hi; tx++ {
		// The segment's y range where its x is within this column, widened by the margin.
		left, right := float64(tx)*tileSize-marginPx, float64(tx+1)*tileSize+marginPx
		ya, yb := y1, y2
		if x1 != x2 {
			ta := math.Max(0, math.Min(1, (left-x1)/(x2-x1)))
			tb := math.Max(0, math.Min(1, (right-x1)/(x2-x1)))
			ya, yb = y1+(y2-y1)*ta, y1+(y2-y1)*tb
		}
		top := clamp(int(math.Floor((math.Min(ya, yb) - marginPx) / tileSize)))
		bottom := clamp(int(math.Floor((math.Max(ya, yb) + marginPx) / tileSize)))
		for ty := top; ty <= bottom; ty++ {
			out = append(out, [2]int{wrap(tx), ty})
		}
	}
	return out
}
