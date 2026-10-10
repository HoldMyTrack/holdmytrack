package geo

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedStats is what one run of the seed did, for its log line.
type SeedStats struct {
	Countries int64
	Regions   int64
	// Regions whose country isn't in the file, left out.
	SkippedRegions int64
	// Countries with no region of their own (Aruba, Antarctica, a disputed area), given one
	// that is the whole country, so the Region tier veils and reveals them too.
	WholeCountryRegions int64
	ActivityCountries   int64
	ActivityRegions     int64
	// The extract was the one already loaded, so nothing was done.
	Unchanged bool
}

// The fewest outlines a world extract has: 2026-09's has 272 countries and dependencies and
// 3,922 regions. Fewer means a cut-short or mis-made file, which would otherwise wipe out the
// rest of the world's outlines and every activity's matches with them.
const (
	minCountries = 250
	minRegions   = 3500
)

// How far, in degrees, the outlines the Country and Region tiles draw may stray from the
// full-detail ones matching uses: about 5 km for countries, drawn below z3 (some 10 km a pixel
// at the equator), and 1 km for regions, drawn to z6 (1.2 km a pixel). Finer outlines made the
// world's z0 tile take over a second to draw on every request (IMPLEMENTATION.md §4.2.4).
const (
	countryTolerance = 0.05
	regionTolerance  = 0.01
)

// SeedAdminBoundaries replaces every country and region outline with the extract's and
// re-matches every activity against them, in one transaction, so the tiles and matches go from
// the old outlines to the new without a moment of neither. name is the extract's file name:
// a run whose name matches the extract already loaded does nothing, unless force is set.
//
// Registered as the `seed-admin-boundaries` subcommand (cmd/holdmytrack/main.go). r is the
// gzipped CSV scripts/boundaries-extract.sh writes.
func SeedAdminBoundaries(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, r io.Reader, name string, force bool) (SeedStats, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return SeedStats{}, err
	}
	defer tx.Rollback(ctx)
	stats, err := loadBoundaries(ctx, tx, log, r, name, force, minCountries, minRegions)
	if err != nil || stats.Unchanged {
		return stats, err
	}
	return stats, tx.Commit(ctx)
}

func loadBoundaries(ctx context.Context, tx pgx.Tx, log *slog.Logger, r io.Reader, name string, force bool, wantCountries, wantRegions int64) (SeedStats, error) {
	var stats SeedStats
	if !force {
		var loaded string
		err := tx.QueryRow(ctx, `SELECT name FROM admin_boundaries_source`).Scan(&loaded)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return stats, err
		}
		if loaded == name {
			stats.Unchanged = true
			return stats, nil
		}
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return stats, fmt.Errorf("geo: read %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS pg_temp.boundary_csv, pg_temp.boundary`); err != nil {
		return stats, err
	}
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE boundary_csv (kind TEXT, key TEXT, country TEXT, code TEXT, name TEXT, geom TEXT) ON COMMIT DROP
	`); err != nil {
		return stats, err
	}
	if _, err := tx.Conn().PgConn().CopyFrom(ctx, gz, `COPY boundary_csv FROM STDIN (FORMAT csv)`); err != nil {
		return stats, fmt.Errorf("geo: load %s: %w", name, err)
	}
	var countries, regions int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE kind = 'country'), count(*) FILTER (WHERE kind = 'region') FROM boundary_csv
	`).Scan(&countries, &regions); err != nil {
		return stats, err
	}
	if countries < wantCountries || regions < wantRegions {
		return stats, fmt.Errorf("geo: %s has %d countries and %d regions, fewer than a whole world's %d and %d: cut short?",
			name, countries, regions, wantCountries, wantRegions)
	}
	log.Info("seed-admin-boundaries: read", "file", name, "countries", countries, "regions", regions)

	// Polygons only, made valid first: ST_Subdivide and ST_Intersects need valid input, and a
	// border OpenStreetMap draws touching itself isn't always.
	steps := []struct {
		what string
		sql  string
		into *int64
	}{
		{"outlines", `
			CREATE TEMP TABLE boundary ON COMMIT DROP AS
			SELECT kind, key, country, NULLIF(code, '') AS code, name,
			       ST_Multi(ST_CollectionExtract(ST_MakeValid(ST_SetSRID(geom::geometry, 4326)), 3)) AS geom
			FROM boundary_csv`, nil},
		{"old matches", `DELETE FROM activity_region`, nil},
		{"", `DELETE FROM activity_country`, nil},
		{"old outlines", `DELETE FROM admin_regions`, nil},
		{"", `DELETE FROM admin_countries`, nil},
		{"countries", `
			INSERT INTO admin_countries (code, name, geom)
			SELECT key, name, ` + displayGeom("geom", countryTolerance) + ` FROM boundary WHERE kind = 'country'`, &stats.Countries},
		{"", `
			INSERT INTO admin_country_parts (country_id, geom)
			SELECT c.id, (ST_Dump(ST_Subdivide(b.geom, 256))).geom
			FROM boundary b JOIN admin_countries c ON c.code = b.key WHERE b.kind = 'country'`, nil},
		{"regions", `
			INSERT INTO admin_regions (country_id, overture_id, code, name, geom)
			SELECT c.id, b.key, b.code, b.name, ` + displayGeom("b.geom", regionTolerance) + `
			FROM boundary b JOIN admin_countries c ON c.code = b.country WHERE b.kind = 'region'`, &stats.Regions},
		{"", `
			INSERT INTO admin_region_parts (region_id, geom)
			SELECT r.id, (ST_Dump(ST_Subdivide(b.geom, 256))).geom
			FROM boundary b JOIN admin_regions r ON r.overture_id = b.key WHERE b.kind = 'region'`, nil},
		{"whole-country regions", `
			INSERT INTO admin_regions (country_id, overture_id, code, name, geom)
			SELECT c.id, 'country:' || c.code, NULL, c.name, ` + displayGeom("b.geom", regionTolerance) + `
			FROM admin_countries c JOIN boundary b ON b.kind = 'country' AND b.key = c.code
			WHERE NOT EXISTS (SELECT 1 FROM admin_regions r WHERE r.country_id = c.id)`, &stats.WholeCountryRegions},
		{"", `
			INSERT INTO admin_region_parts (region_id, geom)
			SELECT r.id, p.geom
			FROM admin_regions r JOIN admin_countries c ON 'country:' || c.code = r.overture_id
			JOIN admin_country_parts p ON p.country_id = c.id`, nil},
		{"activity countries", `
			INSERT INTO activity_country (activity_id, country_id)
			SELECT DISTINCT a.id, p.country_id FROM activities a JOIN admin_country_parts p ON ST_Intersects(p.geom, ` + WrappedSQL("a.trajectory") + `)`, &stats.ActivityCountries},
		{"activity regions", `
			INSERT INTO activity_region (activity_id, region_id)
			SELECT DISTINCT a.id, p.region_id FROM activities a JOIN admin_region_parts p ON ST_Intersects(p.geom, ` + WrappedSQL("a.trajectory") + `)`, &stats.ActivityRegions},
	}
	for _, s := range steps {
		tag, err := tx.Exec(ctx, s.sql)
		if err != nil {
			return stats, fmt.Errorf("geo: %s: %w", s.what, err)
		}
		if s.into != nil {
			*s.into = tag.RowsAffected()
		}
		if s.what != "" {
			log.Info("seed-admin-boundaries: "+s.what, "rows", tag.RowsAffected())
		}
	}
	stats.SkippedRegions = regions - stats.Regions

	if _, err := tx.Exec(ctx, `
		INSERT INTO admin_boundaries_source (name) VALUES ($1)
		ON CONFLICT (only_row) DO UPDATE SET name = EXCLUDED.name, loaded_at = NOW()
	`, name); err != nil {
		return stats, err
	}
	// Every account's Country/Region tiles change, so their cached copies have to go (§4.2.6).
	if _, err := tx.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
		return stats, err
	}
	// And so do the outlines the tiles keep, drawn from the old ones (internal/httpapi's
	// serveAdminTile). Last, so the lock TRUNCATE takes holds tile requests back only until this
	// commits, and a tile built meanwhile waits for it and draws the new outlines.
	if _, err := tx.Exec(ctx, `TRUNCATE admin_tiles_built, admin_tile_geoms`); err != nil {
		return stats, err
	}
	return stats, nil
}

// displayGeom is the SQL for an outline the tiles draw: col simplified to tolerance, dropping
// the islets and rings that collapse, which at these zooms are under a pixel, and made valid
// again, since plain simplifying can cross a ring over itself. Where nothing is left (Vatican
// City, Monaco: ST_Simplify gives NULL), the full outline, which is tiny anyway.
func displayGeom(col string, tolerance float64) string {
	s := fmt.Sprintf("ST_Multi(ST_CollectionExtract(ST_MakeValid(ST_Simplify(%s, %v)), 3))", col, tolerance)
	return fmt.Sprintf("CASE WHEN ST_IsEmpty(%[1]s) IS NOT FALSE THEN %[2]s ELSE %[1]s END", s, col)
}
