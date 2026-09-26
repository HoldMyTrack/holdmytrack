package httpapi

import "net/http"

// regionFogQuery/regionHeatmapQuery mirror admin_country_tiles.go's countryFogQuery/
// countryHeatmapQuery exactly, one level down — see that file's doc comments for the
// reasoning shared by both (live query over a small fixed polygon count, and heatmap's
// in_heatmap_window carry-through).
const regionFogQuery = `
SELECT ST_AsMVT(t, 'regions', 4096, 'geom')
FROM (
    SELECT r.id,
           ST_AsMVTGeom(
               ST_Transform(r.geom, 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM admin_regions r
    WHERE r.geom && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND NOT EXISTS (
          SELECT 1 FROM activity_region ar
          JOIN activities a ON a.id = ar.activity_id
          WHERE ar.region_id = r.id AND a.user_id = $4 AND a.superseded_by IS NULL AND NOT a.edit_pending
      )
) t;`

const regionHeatmapQuery = `
SELECT ST_AsMVT(t, 'regions', 4096, 'geom')
FROM (
    SELECT r.id,
           ST_AsMVTGeom(
               ST_Transform(r.geom, 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM admin_regions r
    WHERE r.geom && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND EXISTS (
          SELECT 1 FROM activity_region ar
          JOIN activities a ON a.id = ar.activity_id
          WHERE ar.region_id = r.id AND a.user_id = $4 AND a.superseded_by IS NULL AND NOT a.edit_pending
            AND a.in_heatmap_window
      )
) t;`

func (s *Server) handleRegionFogTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, regionFogQuery)
}

func (s *Server) handleRegionHeatmapTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, regionHeatmapQuery)
}
