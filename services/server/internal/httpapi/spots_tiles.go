package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// spotsMinZoom is the lowest zoom the spots tiles carry anything at: a few neighbourhoods in
// view. Below it a city's playgrounds and memorials would be one pile of icons.
const spotsMinZoom = 12

// spotsQuery is §4.25's tile, two layers over the same spots:
//   - `spots`, one point per spot whose anchor falls in the tile — a point on its area, so a
//     spot spanning two tiles is drawn once — with what the popup shows: name, category,
//     address, OSM's text fields and Wikipedia article, and the anchor's coordinates;
//   - `spot_areas`, each spot's area clipped to the tile, with its category (the Overlays menu
//     filters both layers by it) and `circle` for the 30 m circle a place mapped as a single
//     node stands in with, rather than an outline OSM has.
//
// The two layers are separate ST_AsMVT calls concatenated, which is a valid tile: a tile is a
// list of layers.
const spotsQuery = `
WITH env AS (SELECT ST_TileEnvelope($1, $2, $3) AS e3857, ST_Transform(ST_TileEnvelope($1, $2, $3), 4326) AS e),
here AS MATERIALIZED (
    SELECT s.*, ST_PointOnSurface(s.geom) AS pt
    FROM env, spots s
    WHERE s.geom && env.e
)
SELECT
    (SELECT ST_AsMVT(t, 'spots', 4096, 'geom') FROM (
        SELECT h.id, h.category, h.name, h.address, h.description, h.inscription, h.memorial,
               h.start_date, h.wikipedia, ST_X(h.pt) AS lon, ST_Y(h.pt) AS lat,
               ST_AsMVTGeom(ST_Transform(h.pt, 3857), env.e3857, 4096, 0, true) AS geom
        FROM here h, env
        WHERE ST_Intersects(h.pt, env.e)
    ) t)
    ||
    (SELECT ST_AsMVT(t, 'spot_areas', 4096, 'geom') FROM (
        SELECT h.id, h.category, h.osm_type = 'node' AS circle,
               ST_AsMVTGeom(ST_Transform(h.geom, 3857), env.e3857, 4096, 64, true) AS geom
        FROM here h, env
    ) t);`

// handleSpotsTile serves `GET /tiles/v1/spots/{z}/{x}/{y}.mvt`. The places are the same for
// everyone, but the tiles stay behind the session like every other map route, and are cached
// under the account's tile version (setTileCacheControl), which the Spots import bumps for every
// account.
func (s *Server) handleSpotsTile(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".mvt"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}

	var tile []byte
	if z >= spotsMinZoom {
		if err := s.pool.QueryRow(r.Context(), spotsQuery, z, x, y).Scan(&tile); err != nil {
			s.log.Error("spots tile query failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(tile)
}
