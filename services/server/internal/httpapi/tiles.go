package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// tracksQuery is IMPLEMENTATION.md §4.3 verbatim, with two changes: user_id
// is the authenticated caller (userIDFromContext — requireAuth, server.go), and from/to/types
// each become "$n IS NULL OR ..." so an absent query param means "no filter" instead of
// requiring the caller to pass an explicit wide-open range.
const tracksQuery = `
SELECT ST_AsMVT(t, 'tracks', 4096, 'geom')
FROM (
    SELECT id,
           activity_type,
           ST_AsMVTGeom(
               ST_Transform(ST_Force2D(trajectory), 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM activities
    WHERE user_id = $4
      AND trajectory && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND ($5::timestamptz IS NULL OR started_at >= $5)
      AND ($6::timestamptz IS NULL OR started_at <= $6)
      AND ($7::text[] IS NULL OR activity_type = ANY($7))
) t;`

const dateLayout = "2006-01-02"

// handleTracksTile serves §4.3's live MVT query. Unlike fog, tracks are never precomputed:
// they have to answer filters (date range, activity type) that can't be baked into a raster
// ahead of time, so this runs the query per request rather than reading a stored tile.
//
// Registered as "GET /tiles/v1/tracks/{z}/{x}/{y}" (no ".mvt" in the pattern) rather than
// "{y}.mvt" — verified against the installed Go toolchain that net/http's ServeMux wildcard
// must occupy a whole path segment, so a literal suffix glued onto "{y}" is not a pattern
// stdlib supports. The ".mvt" suffix is trimmed from the captured segment by hand instead.
func (s *Server) handleTracksTile(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".mvt"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}

	// The same from/to/types shape §4.7's listing endpoints parse — one parser, so the
	// "absent means no restriction" convention can't drift between the map and the list.
	filter, err := parseActivityFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tile []byte
	err = s.pool.QueryRow(r.Context(), tracksQuery, z, x, y, userIDFromContext(r.Context()), filter.From, filter.To, filter.Types).Scan(&tile)
	if err != nil {
		s.log.Error("tracks tile query failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// A tile with no matching activities scans as a zero-length (non-nil) []byte, verified
	// directly against a live instance — ST_AsMVT still returns one row over zero input
	// rows, just with an empty bytea, not NULL and not an error. Writing zero bytes with a
	// 200 is the correct "no data here" response; MapLibre treats it as an empty tile.
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	_, _ = w.Write(tile)
}
