package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// spotsMinZoom is the lowest zoom the spots tiles carry anything at (ADR-0021): below it a
// city's playgrounds and memorials would be hundreds of icons on top of each other.
const spotsMinZoom = 14

// spotsQuery is §4.25's tile: one point per spot whose anchor falls in the tile — a point on
// its area, so a spot spanning two tiles is drawn once — with what the popup shows (name,
// category, address, the anchor's coordinates) and whether the caller has visited it. A visit
// counts only through a live activity: a superseded duplicate's visits are kept, like its
// masks, but read past, the same way every other map read does.
const spotsQuery = `
WITH env AS (SELECT ST_Transform(ST_TileEnvelope($1, $2, $3), 4326) AS e)
SELECT ST_AsMVT(t, 'spots', 4096, 'geom')
FROM (
    SELECT s.id, s.category, s.name, s.address,
           ST_X(a.pt) AS lon, ST_Y(a.pt) AS lat,
           EXISTS (
               SELECT 1 FROM spot_visits v JOIN activities act ON act.id = v.activity_id
               WHERE v.user_id = $4 AND v.spot_id = s.id AND act.superseded_by IS NULL
           ) AS visited,
           ST_AsMVTGeom(ST_Transform(a.pt, 3857), ST_TileEnvelope($1, $2, $3), 4096, 0, true) AS geom
    FROM env, spots s
    CROSS JOIN LATERAL (SELECT ST_PointOnSurface(s.geom) AS pt) a
    WHERE s.geom && env.e
      AND ST_Intersects(a.pt, env.e)
) t;`

// handleSpotsTile serves `GET /tiles/v1/spots/{z}/{x}/{y}.mvt`. The places are the same for
// everyone, but the visited flag is the caller's — the account comes from the session, never a
// parameter — so this is a per-user tile, cached under the account's tile version like the
// tracks tiles (setTileCacheControl). Processing an activity bumps that version when its
// render lands, and the Spots import and its backfill bump every account's.
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
		if err := s.pool.QueryRow(r.Context(), spotsQuery, z, x, y, userIDFromContext(r.Context())).Scan(&tile); err != nil {
			s.log.Error("spots tile query failed", "err", err, "z", z, "x", x, "y", y)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(tile)
}
