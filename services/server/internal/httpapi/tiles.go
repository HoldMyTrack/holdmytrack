package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/geo"
)

// tracksQuery is IMPLEMENTATION.md §4.3 verbatim, with three changes: user_id
// is the authenticated caller (userIDFromContext — requireAuth, server.go), from/to/types/story
// each become "$n IS NULL OR ..." so an absent query param means "no filter" instead of
// requiring the caller to pass an explicit wide-open range, and a Pending activity
// (edit_pending, §4.7.7) is left out — its trajectory is the pre-reprocess one, and it comes
// back once the reprocess lands.
//
// A track across the antimeridian is stored continuing past ±180 (§4.1). It's drawn wrapped
// back into −180…180 (geo.WrappedSQL: a part each side of the antimeridian), since PROJ itself
// wraps a longitude past 180 when projecting and would join the parts the long way round the
// world again; only such a track pays for the wrap. It matches a tile on the far side of the
// antimeridian through the tile moved a world's width, which keeps each test an index lookup.
var tracksQuery = `
SELECT ST_AsMVT(t, 'tracks', 4096, 'geom')
FROM (
    SELECT id,
           activity_type,
           ST_AsMVTGeom(
               ST_Transform(ST_Force2D(
                   CASE WHEN ST_XMin(trajectory) < -180 OR ST_XMax(trajectory) > 180
                        THEN ` + geo.WrappedSQL("trajectory") + ` ELSE trajectory END), 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM activities, ST_Transform(ST_TileEnvelope($1, $2, $3), 4326) AS env
    WHERE user_id = $4
      AND NOT edit_pending
      AND (trajectory && env OR trajectory && ST_Translate(env, 360, 0) OR trajectory && ST_Translate(env, -360, 0))
      AND ` + inDateRange("$5", "$6") + `
      AND ($7::text[] IS NULL OR activity_type = ANY($7))
      AND ($8::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $8))
) t;`

const dateLayout = "2006-01-02"

// maxTileZoom is past any zoom the clients ask for (the Fog and Heatmap rasters stop at z14
// and are overzoomed above it).
const maxTileZoom = 24

// tileCoords reads a tile route's {z}/{x}/{y}, with ext trimmed off {y}, or answers 400 for
// coordinates that aren't a tile: not numbers, a zoom outside 0..maxTileZoom, or an x or y
// outside 0..2^z-1. Passed to ST_TileEnvelope, an out-of-range tile was a query error, a 500
// and an error-level log line per request.
func tileCoords(w http.ResponseWriter, r *http.Request, ext string) (z, x, y int, ok bool) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ext))
	if errZ != nil || errX != nil || errY != nil || z < 0 || z > maxTileZoom ||
		x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return 0, 0, 0, false
	}
	return z, x, y, true
}

// handleTracksTile serves §4.3's live MVT query. Unlike fog, tracks are never precomputed:
// they have to answer filters (date range, activity type) that can't be baked into a raster
// ahead of time, so this runs the query per request rather than reading a stored tile.
//
// Registered as "GET /tiles/v1/tracks/{z}/{x}/{y}" (no ".mvt" in the pattern) rather than
// "{y}.mvt" — verified against the installed Go toolchain that net/http's ServeMux wildcard
// must occupy a whole path segment, so a literal suffix glued onto "{y}" is not a pattern
// stdlib supports. The ".mvt" suffix is trimmed from the captured segment by hand instead.
func (s *Server) handleTracksTile(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := tileCoords(w, r, ".mvt")
	if !ok {
		return
	}

	// The same from/to/types/story shape §4.7's listing endpoints parse — one parser, so the
	// "absent means no restriction" convention can't drift between the map and the list.
	filter, err := parseActivityFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tile []byte
	err = s.pool.QueryRow(r.Context(), tracksQuery, z, x, y, userIDFromContext(r.Context()), filter.From, filter.To, filter.Types, filter.Story).Scan(&tile)
	if err != nil {
		if clientGone(w, r) {
			return
		}
		s.log.Error("tracks tile query failed", "err", err, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// A tile with no matching activities scans as a zero-length (non-nil) []byte, verified
	// directly against a live instance — ST_AsMVT still returns one row over zero input
	// rows, just with an empty bytea, not NULL and not an error. Writing zero bytes with a
	// 200 is the correct "no data here" response; MapLibre treats it as an empty tile.
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(tile)
}
