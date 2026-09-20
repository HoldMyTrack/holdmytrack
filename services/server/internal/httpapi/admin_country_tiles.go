package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// countryFogQuery answers "is this country still veiled" per docs/IMPLEMENTATION.md §4.2.4 —
// the locked side (no matching activity at all) of the Fog Country tier. Deliberately a live
// MVT query, not a precomputed raster like fog/heatmap's own city-tier tiles: admin_countries
// has a small, fixed row count (~250), so a per-request NOT EXISTS join against
// activity_country (populated once at ingest by internal/geo.MatchActivity, never at request
// time) costs nothing like the old live-heatmap-compositing path did, which scaled with a
// user's own activity count instead, not a fixed polygon count.
const countryFogQuery = `
SELECT ST_AsMVT(t, 'countries', 4096, 'geom')
FROM (
    SELECT c.id,
           ST_AsMVTGeom(
               ST_Transform(c.geom, 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM admin_countries c
    WHERE c.geom && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND NOT EXISTS (
          SELECT 1 FROM activity_country ac
          JOIN activities a ON a.id = ac.activity_id
          WHERE ac.country_id = c.id AND a.user_id = $4 AND a.superseded_by IS NULL
      )
) t;`

// countryHeatmapQuery is the unlocked side of the Heatmap Country tier: at least one activity
// inside the country, and — matching heatmap_tiles.go's own rolling-window behavior at city
// zoom — still `in_heatmap_window`, so a country visited only long ago cools off here exactly
// as it already does at the per-pixel tier instead of the two disagreeing at the zoom
// boundary.
const countryHeatmapQuery = `
SELECT ST_AsMVT(t, 'countries', 4096, 'geom')
FROM (
    SELECT c.id,
           ST_AsMVTGeom(
               ST_Transform(c.geom, 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM admin_countries c
    WHERE c.geom && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND EXISTS (
          SELECT 1 FROM activity_country ac
          JOIN activities a ON a.id = ac.activity_id
          WHERE ac.country_id = c.id AND a.user_id = $4 AND a.superseded_by IS NULL
            AND a.in_heatmap_window
      )
) t;`

func (s *Server) handleCountryFogTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, countryFogQuery)
}

func (s *Server) handleCountryHeatmapTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, countryHeatmapQuery)
}

// serveAdminTile is shared by all four admin-boundary tile handlers (this file's pair and
// admin_region_tiles.go's) — they differ only in which fixed query string they pass, not in
// how tile coordinates are parsed or the result written.
//
// Registered without ".mvt" in the route pattern, same as handleTracksTile — net/http's
// ServeMux wildcard has to occupy a whole path segment, so the suffix is trimmed by hand here
// too.
func (s *Server) serveAdminTile(w http.ResponseWriter, r *http.Request, query string) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".mvt"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}

	var tile []byte
	err := s.pool.QueryRow(r.Context(), query, z, x, y, userIDFromContext(r.Context())).Scan(&tile)
	if err != nil {
		s.log.Error("admin tile query failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	_, _ = w.Write(tile)
}
