package httpapi

import "net/http"

// The Region tier's handlers mirror admin_country_tiles.go's Country pair exactly, one level
// down — see that file for the queries and the cache they share.

func (s *Server) handleRegionFogTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, regionLayer, regionFogQueries)
}

func (s *Server) handleRegionHeatmapTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, regionLayer, regionHeatmapQueries)
}
