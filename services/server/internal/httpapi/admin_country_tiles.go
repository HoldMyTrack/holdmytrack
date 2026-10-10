package httpapi

import (
	"context"
	"fmt"
	"net/http"
)

// adminLayer is one of the two outline sets the Country and Region tiers draw
// (docs/IMPLEMENTATION.md §4.2.4): which table holds the outlines, which holds each
// activity's matches to them (populated once at ingest by internal/geo.MatchActivity, never at
// request time), and the tile's MVT layer name, which also keys its rows in admin_tile_geoms.
type adminLayer struct {
	name     string
	outlines string
	matches  string
	matchID  string
	// cacheMaxZoom is the highest zoom whose tiles are kept in admin_tile_geoms: the source's
	// maxzoom on both clients (apps/web/src/map/zoomTiers.ts). A tile past it, which no client
	// asks for, is drawn live rather than letting anyone fill the table with tiles nothing
	// draws.
	cacheMaxZoom int
}

var (
	countryLayer = adminLayer{name: "countries", outlines: "admin_countries", matches: "activity_country", matchID: "country_id", cacheMaxZoom: 3}
	regionLayer  = adminLayer{name: "regions", outlines: "admin_regions", matches: "activity_region", matchID: "region_id", cacheMaxZoom: 7}
)

// liveGeoms draws every outline in tile ($1, $2, $3), transformed and clipped to it. The same
// for every account, so serveAdminTile keeps it in admin_tile_geoms rather than drawing it on
// every request: transforming and clipping is most of a tile's cost (docs/PERFORMANCE.md,
// 2026-10-09 dev, over 80 ms for the world's z0 Country tile).
func (l adminLayer) liveGeoms() string {
	return fmt.Sprintf(`
		SELECT o.id AS admin_id,
		       ST_AsMVTGeom(ST_Transform(o.geom, 3857), ST_TileEnvelope($1, $2, $3), 4096, 64, true) AS geom
		FROM %s o
		WHERE o.geom && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)`, l.outlines)
}

// tileQuery is one account's tile ($1, $2, $3) of this layer, for account $4: Fog's shows the
// outlines the account has no activity in (the locked side), Heatmap's those it has one in
// that is still `in_heatmap_window`, so a country visited only long ago cools off here exactly
// as it does at the per-pixel tier instead of the two disagreeing at the zoom boundary.
//
// visited starts from the account's own activities, so its cost is the account's history,
// not every account's matches in the outlines the tile covers. It answers two columns: whether
// the tile is in admin_tile_geoms yet (always true for a live one) and the tile itself, its
// features in outline order whichever way it was drawn.
func (l adminLayer) tileQuery(heatmap, cached bool) string {
	window, test := "", "NOT EXISTS"
	if heatmap {
		window, test = "AND a.in_heatmap_window", "EXISTS"
	}
	built, geoms := "true", l.liveGeoms()
	if cached {
		built = fmt.Sprintf(`EXISTS (
			SELECT 1 FROM admin_tiles_built
			WHERE layer = '%s' AND zoom = $1 AND tile_x = $2 AND tile_y = $3)`, l.name)
		geoms = fmt.Sprintf(`
			SELECT admin_id, geom FROM admin_tile_geoms
			WHERE layer = '%s' AND zoom = $1 AND tile_x = $2 AND tile_y = $3`, l.name)
	}
	return fmt.Sprintf(`
		WITH visited AS MATERIALIZED (
			SELECT DISTINCT m.%[1]s AS id
			FROM activities a JOIN %[2]s m ON m.activity_id = a.id
			WHERE a.user_id = $4 AND NOT a.edit_pending %[3]s
		)
		SELECT %[4]s,
		       (SELECT ST_AsMVT(t, '%[5]s', 4096, 'geom')
		        FROM (
		            SELECT g.admin_id AS id, g.geom
		            FROM (%[6]s) g
		            WHERE %[7]s (SELECT 1 FROM visited v WHERE v.id = g.admin_id)
		            ORDER BY g.admin_id
		        ) t)`, l.matchID, l.matches, window, built, l.name, geoms, test)
}

// adminTileQueries are the four handlers' queries, cached and live, built once.
type adminTileQueries struct{ cached, live string }

var (
	countryFogQueries     = adminTileQueries{countryLayer.tileQuery(false, true), countryLayer.tileQuery(false, false)}
	countryHeatmapQueries = adminTileQueries{countryLayer.tileQuery(true, true), countryLayer.tileQuery(true, false)}
	regionFogQueries      = adminTileQueries{regionLayer.tileQuery(false, true), regionLayer.tileQuery(false, false)}
	regionHeatmapQueries  = adminTileQueries{regionLayer.tileQuery(true, true), regionLayer.tileQuery(true, false)}
)

func (s *Server) handleCountryFogTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, countryLayer, countryFogQueries)
}

func (s *Server) handleCountryHeatmapTile(w http.ResponseWriter, r *http.Request) {
	s.serveAdminTile(w, r, countryLayer, countryHeatmapQueries)
}

// serveAdminTile is shared by all four admin-boundary tile handlers (this file's pair and
// admin_region_tiles.go's) — they differ only in the layer and its queries, not in how tile
// coordinates are parsed or the result written. A tile up to the layer's cacheMaxZoom is read
// from admin_tile_geoms, built there by its first request (buildAdminTile).
//
// Registered without ".mvt" in the route pattern, same as handleTracksTile — net/http's
// ServeMux wildcard has to occupy a whole path segment, so the suffix is trimmed by hand here
// too.
func (s *Server) serveAdminTile(w http.ResponseWriter, r *http.Request, l adminLayer, q adminTileQueries) {
	z, x, y, ok := tileCoords(w, r, ".mvt")
	if !ok {
		return
	}
	ctx := r.Context()
	query := q.live
	if z <= l.cacheMaxZoom {
		query = q.cached
	}

	var built bool
	var tile []byte
	err := s.pool.QueryRow(ctx, query, z, x, y, userIDFromContext(ctx)).Scan(&built, &tile)
	if err == nil && !built {
		if err = s.buildAdminTile(ctx, l, z, x, y); err == nil {
			err = s.pool.QueryRow(ctx, query, z, x, y, userIDFromContext(ctx)).Scan(&built, &tile)
		}
	}
	if err != nil {
		if clientGone(w, r) {
			return
		}
		s.log.Error("admin tile query failed", "err", err, "layer", l.name, "z", z, "x", x, "y", y)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	setTileCacheControl(w, r)
	_, _ = w.Write(tile)
}

// buildAdminTile stores one tile's outlines in admin_tile_geoms and marks it built, in one
// transaction. The mark goes first: a second request building the same tile at once waits on
// it, then finds it taken and leaves the tile to the first. The outlines are read by a
// statement after the mark, so a build that waited on seed-admin-boundaries' TRUNCATE draws
// the new outlines, not the ones it replaced.
func (s *Server) buildAdminTile(ctx context.Context, l adminLayer, z, x, y int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed
	tag, err := tx.Exec(ctx, `
		INSERT INTO admin_tiles_built (layer, zoom, tile_x, tile_y) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, l.name, z, x, y)
	if err != nil {
		return fmt.Errorf("mark admin tile built: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO admin_tile_geoms (layer, zoom, tile_x, tile_y, admin_id, geom)
		SELECT $4, $1, $2, $3, g.admin_id, g.geom
		FROM (`+l.liveGeoms()+`) g
		WHERE g.geom IS NOT NULL`, z, x, y, l.name); err != nil {
		return fmt.Errorf("build admin tile: %w", err)
	}
	return tx.Commit(ctx)
}
