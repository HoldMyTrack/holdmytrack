// Package bikepaths is the Bike paths and Shared paths overlay's data (IMPLEMENTATION.md §3.27,
// §4.24): OpenStreetMap's cycleways and bike-designated paths, imported from an extract and
// refreshed from a newer one, drawn from zoom 9 where the basemap has none.
package bikepaths

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// importBatch is how many ways one INSERT carries, as in the Spots import.
const importBatch = 1000

// maxDeletedShare is the most of the stored ways one planet import may delete. A quarter's real
// churn in OSM is well under it; more is far likelier a planet file cut short or a filter gone
// wrong, so the import stops rather than take most of the map with it.
const maxDeletedShare = 0.01

// Kinds, as stored in bike_paths.kind and sent in the tiles.
const (
	KindCycleway = "cycleway"
	KindShared   = "shared"
)

// DB is what Import needs of the database: a *pgxpool.Pool, or a pgx.Tx in a test.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ImportStats is what Import reports back for the log.
type ImportStats struct {
	Imported int // upserted into bike_paths
	Skipped  int // features of neither kind, with no way id or no line
	Changed  int // of Imported, ways new or different from what was stored
	Deleted  int // ways a planet import no longer saw
}

// ErrTooManyDeleted is Import refusing to delete more than maxDeletedShare of the stored ways.
var ErrTooManyDeleted = errors.New("bikepaths: the file is missing too many ways to be the whole planet")

// Import upserts every bike path in r into bike_paths and, when planet says r is the whole
// planet, deletes the ways it didn't have. Then, if anything was added, changed or deleted, it
// bumps every account's tile version, since every account's bike-path tiles just changed.
//
// r is a GeoJSON Text Sequence as `osmium export -f geojsonseq -u type_id` writes it
// (scripts/bike-paths-extract.sh): one feature per line, its OSM tags as properties, its id
// "w123". Upserted by the way's id, so re-running it on the same or a newer extract updates the
// ways already there. Nothing refers to a row, so a way gone from OSM is deleted, not retired as
// a Spots place is — but only by a planet run, since a regional file would otherwise delete the
// rest of the world, and not past maxDeletedShare of the stored ways (ErrTooManyDeleted).
//
// Registered as the `import-bike-paths` subcommand (cmd/holdmytrack/main.go).
func Import(ctx context.Context, db DB, log *slog.Logger, r io.Reader, planet bool) (ImportStats, error) {
	return importWays(ctx, db, log, r, planet, maxDeletedShare)
}

func importWays(ctx context.Context, db DB, log *slog.Logger, r io.Reader, planet bool, maxDeleted float64) (ImportStats, error) {
	var stats ImportStats
	// The run's stamp, from the database's clock; clock_timestamp, not now(), for the reason
	// spots.Import gives.
	var run time.Time
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&run); err != nil {
		return stats, fmt.Errorf("bikepaths: start: %w", err)
	}
	var batch []way
	batches := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, changed, err := upsertWays(ctx, db, batch, run)
		if err != nil {
			return err
		}
		stats.Imported += n
		stats.Changed += changed
		stats.Skipped += len(batch) - n
		batch = batch[:0]
		if batches++; batches%100 == 0 {
			log.Info("import-bike-paths: progress", "imported", stats.Imported, "skipped", stats.Skipped)
		}
		return nil
	}

	br := bufio.NewReaderSize(r, 1<<20)
	for line := 1; ; line++ {
		raw, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			w, ok, perr := parseFeature(raw)
			if perr != nil {
				return stats, fmt.Errorf("bikepaths: line %d: %w", line, perr)
			}
			if ok {
				batch = append(batch, w)
			} else {
				stats.Skipped++
			}
			if len(batch) == importBatch {
				if ferr := flush(); ferr != nil {
					return stats, ferr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, fmt.Errorf("bikepaths: read: %w", err)
		}
	}
	if err := flush(); err != nil {
		return stats, err
	}

	var deleteErr error
	if planet {
		stats.Deleted, deleteErr = deleteUnseen(ctx, db, run, maxDeleted)
		if deleteErr != nil && !errors.Is(deleteErr, ErrTooManyDeleted) {
			return stats, deleteErr
		}
	}
	// Bumped even when deleting was refused: the ways the run did change are already written.
	if stats.Changed+stats.Deleted > 0 {
		if _, err := db.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
			return stats, fmt.Errorf("bikepaths: bump tile versions: %w", err)
		}
	}
	return stats, deleteErr
}

// deleteUnseen deletes the ways the run didn't stamp, and reports how many — unless that's more
// than maxDeleted of them, when it deletes none and returns ErrTooManyDeleted.
func deleteUnseen(ctx context.Context, db DB, run time.Time, maxDeleted float64) (int, error) {
	var unseen, total int
	if err := db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE last_seen_import <> $1), count(*) FROM bike_paths
	`, run).Scan(&unseen, &total); err != nil {
		return 0, fmt.Errorf("bikepaths: count unseen ways: %w", err)
	}
	if float64(unseen) > maxDeleted*float64(total) {
		return 0, fmt.Errorf("%w: it would delete %d of %d ways, more than %g%%",
			ErrTooManyDeleted, unseen, total, maxDeleted*100)
	}
	tag, err := db.Exec(ctx, `DELETE FROM bike_paths WHERE last_seen_import <> $1`, run)
	if err != nil {
		return 0, fmt.Errorf("bikepaths: delete unseen ways: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// way is one feature, ready to insert. name is "" when OSM has none.
type way struct {
	kind, name string
	geometry   string // GeoJSON
	osmID      int64
}

type feature struct {
	ID         any             `json:"id"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties map[string]any  `json:"properties"`
}

// parseFeature reads one line. ok is false for a feature Import skips: neither kind, not a
// way, or no geometry. A line that isn't JSON at all is an error — the file isn't what the
// script makes.
func parseFeature(raw []byte) (way, bool, error) {
	// RFC 8142 starts each record with a record separator; osmium writes it by default.
	raw = bytes.TrimLeft(raw, "\x1e \t\r\n")
	var f feature
	if err := json.Unmarshal(raw, &f); err != nil {
		return way{}, false, err
	}
	tags := make(map[string]string, len(f.Properties))
	for k, v := range f.Properties {
		if s, ok := v.(string); ok {
			tags[k] = s
		}
	}
	kind := Kind(tags)
	id, idOK := parseWayID(f.ID)
	if kind == "" || !idOK || len(f.Geometry) == 0 || string(f.Geometry) == "null" {
		return way{}, false, nil
	}
	return way{kind: kind, name: strings.TrimSpace(tags["name"]), geometry: string(f.Geometry), osmID: id}, true, nil
}

// Kind maps a way's OSM tags to its kind, or "" for neither: a cycleway (highway=cycleway), or
// a shared path — a path, footway or bridleway marked bicycle=designated. A way under
// construction or proposed carries its future highway value in another key, so it's neither.
func Kind(tags map[string]string) string {
	switch tags["highway"] {
	case "cycleway":
		return KindCycleway
	case "path", "footway", "bridleway":
		if tags["bicycle"] == "designated" {
			return KindShared
		}
	}
	return ""
}

// parseWayID reads osmium's type_id feature id for a way exported as a line, "w123".
func parseWayID(id any) (int64, bool) {
	s, ok := id.(string)
	if !ok || len(s) < 2 || s[0] != 'w' {
		return 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// upsertWays inserts one batch, stamping each way with run, and reports how many rows it wrote
// and how many of those changed: a way new, or different in kind, name or line from what was
// stored. The line is stored in Web Mercator (§3.27). A feature whose geometry isn't a line is
// dropped. DISTINCT ON because one batch can't
// upsert the same row twice; `changed` reads bike_paths as they were before the statement.
func upsertWays(ctx context.Context, db DB, batch []way, run time.Time) (written, changed int, err error) {
	n := len(batch)
	kinds, names, geometries, osmIDs := make([]string, n), make([]string, n), make([]string, n), make([]int64, n)
	for i, w := range batch {
		kinds[i], names[i], geometries[i], osmIDs[i] = w.kind, w.name, w.geometry, w.osmID
	}
	err = db.QueryRow(ctx, `
		WITH incoming AS (
			SELECT DISTINCT ON (osm_id) *
			FROM (
				SELECT r.kind, NULLIF(r.name, '') AS name, r.osm_id,
				       ST_Multi(ST_Transform(ST_SetSRID(ST_GeomFromGeoJSON(r.geojson), 4326), 3857)) AS line
				FROM unnest($1::text[], $2::text[], $3::text[], $4::bigint[]) AS r(kind, name, geojson, osm_id)
			) rows
			WHERE GeometryType(line) = 'MULTILINESTRING' AND NOT ST_IsEmpty(line)
			ORDER BY osm_id
		),
		changed AS (
			SELECT count(*) AS n
			FROM incoming i
			LEFT JOIN bike_paths b ON b.osm_id = i.osm_id
			WHERE b.id IS NULL OR (b.kind, b.name, b.geom) IS DISTINCT FROM (i.kind, i.name, i.line)
		),
		written AS (
			INSERT INTO bike_paths (kind, name, geom, osm_id, last_seen_import)
			SELECT kind, name, line, osm_id, $5 FROM incoming
			ON CONFLICT (osm_id) DO UPDATE SET
				kind = EXCLUDED.kind, name = EXCLUDED.name, geom = EXCLUDED.geom,
				last_seen_import = EXCLUDED.last_seen_import
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM written), (SELECT n FROM changed)
	`, kinds, names, geometries, osmIDs, run).Scan(&written, &changed)
	if err != nil {
		return 0, 0, fmt.Errorf("bikepaths: upsert ways: %w", err)
	}
	return written, changed, nil
}
