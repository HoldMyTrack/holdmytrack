package geo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TimezonesKey is the file seed-timezones reads from the app bucket by default:
// timezone-boundary-builder's timezones-with-oceans.geojson.zip for the release it names,
// uploaded by hand (docs/DEPLOY.md §6). A newer release changes both.
//
// The full set, not the -now variant: that one merges zones whose clocks agree only today, which
// would give an older activity the wrong offset. The ocean zones (Etc/GMT±N) mean a point at sea
// has a zone too.
const TimezonesKey = "timezones/timezones-with-oceans-2026d.geojson.zip"

// The fewest zones a whole-world release has: 2026d has 444 with the ocean ones. Fewer means a
// wrong or cut-short file, which would otherwise move most activities back to their account's zone.
const minTimezones = 400

// TimezoneAtSQL is the SQL for the zone of the point (lon, lat), each a float8 SQL expression:
// the IANA name of the polygon it lies in, or userID's own zone (users.timezone) where there is
// none — before seed-timezones has run, or for a point no polygon covers. Ingest stores it as
// activities.timezone from the activity's first recorded point (IMPLEMENTATION.md §4.30).
func TimezoneAtSQL(lon, lat, userID string) string {
	return `COALESCE(
		(SELECT tzid FROM tz_parts
		 WHERE ST_Intersects(geom, ST_SetSRID(ST_MakePoint(` + lon + `, ` + lat + `), 4326))
		 ORDER BY tzid LIMIT 1),
		(SELECT timezone FROM users WHERE id = ` + userID + `))`
}

// TimezoneStats is what one run of seed-timezones did, for its log line.
type TimezoneStats struct {
	Zones int64
	// Zones the database's own tz data doesn't know, left out: a release newer than the
	// Postgres image's tz data. Their areas fall back to the account's zone until it's updated.
	UnknownZones []string
	Activities   int64
	// The file was the one already loaded, so nothing was done.
	Unchanged bool
}

// SeedTimezones replaces the zone polygons with the file's and re-matches every activity that
// has a track, in one transaction. r is timezone-boundary-builder's GeoJSON zip; name is its
// file name, and a run whose name matches the file already loaded does nothing unless force is
// set.
//
// Re-matching uses the start of the stored trajectory, which ingest's own lookup (the first
// recorded point, before Private locations clip it) can differ from only when an activity
// starts inside a Private location on a zone border. An activity wholly inside one keeps the
// zone it has.
//
// Registered as the `seed-timezones` subcommand (cmd/holdmytrack/main.go).
func SeedTimezones(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, r io.Reader, name string, force bool) (TimezoneStats, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return TimezoneStats{}, err
	}
	defer tx.Rollback(ctx)
	stats, err := loadTimezones(ctx, tx, log, r, name, force, minTimezones)
	if err != nil || stats.Unchanged {
		return stats, err
	}
	return stats, tx.Commit(ctx)
}

func loadTimezones(ctx context.Context, tx pgx.Tx, log *slog.Logger, r io.Reader, name string, force bool, wantZones int64) (TimezoneStats, error) {
	var stats TimezoneStats
	if !force {
		var loaded string
		err := tx.QueryRow(ctx, `SELECT name FROM tz_boundaries_source`).Scan(&loaded)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return stats, err
		}
		if loaded == name {
			stats.Unchanged = true
			return stats, nil
		}
	}

	rows, err := readTimezoneZip(r)
	if err != nil {
		return stats, fmt.Errorf("geo: read %s: %w", name, err)
	}
	if int64(len(rows)) < wantZones {
		return stats, fmt.Errorf("geo: %s has %d zones, fewer than a whole world's %d: wrong file?", name, len(rows), wantZones)
	}
	log.Info("seed-timezones: read", "file", name, "zones", len(rows))

	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS pg_temp.tz_geojson`); err != nil {
		return stats, err
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tz_geojson (tzid TEXT, geom TEXT) ON COMMIT DROP`); err != nil {
		return stats, err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tz_geojson"}, []string{"tzid", "geom"}, pgx.CopyFromRows(rows)); err != nil {
		return stats, fmt.Errorf("geo: load %s: %w", name, err)
	}

	// A zone Postgres can't resolve would make every query that reads the activity fail, so it
	// is left out rather than stored.
	unknown, err := tx.Query(ctx, `
		SELECT tzid FROM tz_geojson WHERE tzid NOT IN (SELECT name FROM pg_timezone_names) ORDER BY tzid
	`)
	if err != nil {
		return stats, err
	}
	stats.UnknownZones, err = pgx.CollectRows(unknown, pgx.RowTo[string])
	if err != nil {
		return stats, err
	}
	if len(stats.UnknownZones) > 0 {
		log.Warn("seed-timezones: zones the database's tz data doesn't know, left out", "zones", strings.Join(stats.UnknownZones, ","))
	}

	steps := []struct {
		what string
		sql  string
		into *int64
	}{
		{"old zones", `DELETE FROM tz_parts`, nil},
		// Polygons only, made valid first: ST_Subdivide and ST_Intersects need valid input.
		{"zones", `
			INSERT INTO tz_parts (tzid, geom)
			SELECT tzid, (ST_Dump(ST_Subdivide(
				ST_CollectionExtract(ST_MakeValid(ST_SetSRID(ST_GeomFromGeoJSON(geom), 4326)), 3), 256))).geom
			FROM tz_geojson WHERE tzid IN (SELECT name FROM pg_timezone_names)`, nil},
		{"activities", `
			UPDATE activities a SET timezone = ` + TimezoneAtSQL("ST_X(ST_StartPoint(a.trajectory))", "ST_Y(ST_StartPoint(a.trajectory))", "a.user_id") + `
			WHERE a.trajectory IS NOT NULL`, &stats.Activities},
	}
	for _, s := range steps {
		tag, err := tx.Exec(ctx, s.sql)
		if err != nil {
			return stats, fmt.Errorf("geo: %s: %w", s.what, err)
		}
		if s.into != nil {
			*s.into = tag.RowsAffected()
		}
		log.Info("seed-timezones: "+s.what, "rows", tag.RowsAffected())
	}
	stats.Zones = int64(len(rows) - len(stats.UnknownZones))

	if _, err := tx.Exec(ctx, `
		INSERT INTO tz_boundaries_source (name) VALUES ($1)
		ON CONFLICT (only_row) DO UPDATE SET name = EXCLUDED.name, loaded_at = NOW()
	`, name); err != nil {
		return stats, err
	}
	return stats, nil
}

// readTimezoneZip reads the one GeoJSON FeatureCollection in a timezone-boundary-builder zip
// into (tzid, geometry as GeoJSON) rows, a feature at a time.
func readTimezoneZip(r io.Reader) ([][]any, error) {
	// zip needs random access; the release's zip is about 56 MB.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var file *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".json") || strings.HasSuffix(f.Name, ".geojson") {
			if file != nil {
				return nil, errors.New("more than one GeoJSON file in the zip")
			}
			file = f
		}
	}
	if file == nil {
		return nil, errors.New("no GeoJSON file in the zip")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	dec := json.NewDecoder(rc)
	// Walk to the "features" array, then decode one feature at a time.
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if key, ok := tok.(string); ok && key == "features" {
			break
		}
		if _, ok := tok.(json.Delim); ok {
			return nil, errors.New("no features array")
		}
		var skip json.RawMessage // another top-level member's value
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	if err := expectDelim(dec, '['); err != nil {
		return nil, err
	}
	var rows [][]any
	for dec.More() {
		var f struct {
			Properties struct {
				Tzid string `json:"tzid"`
			} `json:"properties"`
			Geometry json.RawMessage `json:"geometry"`
		}
		if err := dec.Decode(&f); err != nil {
			return nil, err
		}
		if f.Properties.Tzid == "" || len(f.Geometry) == 0 {
			return nil, errors.New("a feature with no tzid or no geometry")
		}
		rows = append(rows, []any{f.Properties.Tzid, string(f.Geometry)})
	}
	return rows, nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("want %q, got %v", want, tok)
	}
	return nil
}
