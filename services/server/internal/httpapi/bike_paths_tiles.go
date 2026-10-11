package httpapi

import (
	"net/http"
)

// bikePathsMinZoom is the lowest zoom the bike-path tiles carry anything at: a region of
// ~200 km on a screen, far enough out to see which side of town is still unridden, where the
// basemap has no paths at all (IMPLEMENTATION.md §4.24). Must match BIKE_PATHS_MIN_ZOOM in
// apps/web/src/map/bikePaths.ts and MapBikePaths.MIN_ZOOM on Android.
const bikePathsMinZoom = 9

// bikePathsDetailZoom is the zoom from which every way is in the tile however short. Below it a
// way shorter than a pixel — a link between two paths, a stub to a bike rack — is left out: it
// draws as a dot at best, and a city has thousands of them.
const bikePathsDetailZoom = 13

// bikePathsQuery is §4.24's tile: one `bike_paths` layer, a line per way with its `kind`
// (cycleway or shared) and `name`, simplified to half a pixel of the zoom it's drawn at. $4 is
// one pixel's width in Web Mercator meters at the tile's zoom (a 256 px tile); the lines are
// stored in Web Mercator (§3.27), so none is reprojected here.
const bikePathsQuery = `
WITH env AS (SELECT ST_TileEnvelope($1, $2, $3) AS e)
SELECT ST_AsMVT(t, 'bike_paths', 4096, 'geom') FROM (
    SELECT b.kind, b.name,
           ST_AsMVTGeom(ST_Simplify(b.geom, $4 / 2), env.e, 4096, 64, true) AS geom
    FROM env, bike_paths b
    WHERE b.geom && env.e
      AND ($1 >= $5 OR ST_Length(b.geom) >= $4)
) t;`

// webMercatorWorldMeters is the width of the whole Web Mercator plane.
const webMercatorWorldMeters = 40075016.68557849

// handleBikePathsTile serves `GET /tiles/v1/bike-paths/{z}/{x}/{y}.mvt`. Like the spots tiles,
// the ways are the same for everyone but the tiles stay behind the session and are cached under
// the account's tile version (setTileCacheControl), which the import bumps for every account.
func (s *Server) handleBikePathsTile(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := tileCoords(w, r, ".mvt")
	if !ok {
		return
	}

	var tile []byte
	if z >= bikePathsMinZoom {
		pixel := webMercatorWorldMeters / float64(int64(256)<<z)
		if err := s.pool.QueryRow(r.Context(), bikePathsQuery, z, x, y, pixel, bikePathsDetailZoom).Scan(&tile); err != nil {
			if clientGone(w, r) {
				return
			}
			s.log.Error("bike paths tile query failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(tile)
}
