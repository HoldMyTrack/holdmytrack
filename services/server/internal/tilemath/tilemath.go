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
// commute logged by mistake, a GPS glitch) can span several, and a gap here would leave a
// strip of genuinely-covered ground stuck fogged.
func SegmentTiles(lon1, lat1, lon2, lat2 float64, zoom int) [][2]int {
	x1, y1 := LonLatToTile(lon1, lat1, zoom)
	x2, y2 := LonLatToTile(lon2, lat2, zoom)
	return bresenham(x1, y1, x2, y2)
}

// SegmentTilesBuffered is SegmentTiles plus every neighboring tile a marginPx-wide margin
// around either endpoint spills into — the tile a bare coordinate floors into isn't the only
// tile the *rendered* stroke can touch, since fog/heatmap draws each point as a
// strokeRadiusPx-wide, then featherPx-blurred mark, not an infinitesimal dot. A point that
// lands within marginPx (tile-local pixels, at tileSize) of a tile edge has part of that
// drawn mark geometrically inside the neighboring tile even though the point's own bare tile
// index never crosses over — without this, that neighbor is never rendered for the activity
// and gg's rasterizer silently clips the overflow at the canvas edge instead.
func SegmentTilesBuffered(lon1, lat1, lon2, lat2 float64, zoom int, marginPx, tileSize float64) [][2]int {
	seen := map[[2]int]struct{}{}
	for _, t := range SegmentTiles(lon1, lat1, lon2, lat2, zoom) {
		seen[t] = struct{}{}
	}

	n := int(math.Pow(2, float64(zoom)))
	addBufferedNeighbors := func(lon, lat float64) {
		wx, wy := WorldPixel(lon, lat, zoom, tileSize)
		tx, ty := int(math.Floor(wx/tileSize)), int(math.Floor(wy/tileSize))
		localX, localY := wx-float64(tx)*tileSize, wy-float64(ty)*tileSize
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				if dx == 0 && dy == 0 {
					continue
				}
				withinX := dx == 0 || (dx < 0 && localX < marginPx) || (dx > 0 && localX > tileSize-marginPx)
				withinY := dy == 0 || (dy < 0 && localY < marginPx) || (dy > 0 && localY > tileSize-marginPx)
				if !withinX || !withinY {
					continue
				}
				nx, ny := tx+dx, ty+dy
				if nx < 0 || ny < 0 || nx >= n || ny >= n {
					continue
				}
				seen[[2]int{nx, ny}] = struct{}{}
			}
		}
	}
	addBufferedNeighbors(lon1, lat1)
	addBufferedNeighbors(lon2, lat2)

	out := make([][2]int, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	return out
}

// bresenham walks integer tile coordinates from (x0,y0) to (x1,y1) inclusive.
func bresenham(x0, y0, x1, y1 int) [][2]int {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy

	var out [][2]int
	x, y := x0, y0
	for {
		out = append(out, [2]int{x, y})
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
