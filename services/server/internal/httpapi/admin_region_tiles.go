package httpapi

import "net/http"

// The Region tier's handler mirrors admin_country_tiles.go's Country one exactly, one level
// down — see that file for the query and the cache they share.

func (s *Server) handleRegionFogTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, regionLayer, regionFogQueries)
}
