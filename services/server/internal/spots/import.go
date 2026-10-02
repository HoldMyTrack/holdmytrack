// Package spots is Spots (IMPLEMENTATION.md §3.20, §4.25, ADR-0021): outdoor places imported
// from OpenStreetMap and refreshed from it (ADR-0027), shown on the map behind the Show POI toggle.
package spots

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// importBatch is how many places one INSERT carries. Big enough that a planet-sized import
// isn't dominated by round trips, small enough that one batch's geometries stay a few MB.
const importBatch = 1000

// maxRetiredShare is the most of the live places one planet import may retire (ADR-0027). A
// quarter's real churn in OSM is well under it; more is far likelier a planet file cut short or a
// filter gone wrong, so the import stops rather than take most of the map with it.
const maxRetiredShare = 0.01

// DB is what Import needs of the database: a *pgxpool.Pool, or a pgx.Tx in a test.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ImportStats is what Import reports back for the log.
type ImportStats struct {
	Imported int // upserted into spots
	Skipped  int // features with no category, no usable id or no geometry
	Changed  int // of Imported, places new, different from what was stored, or back from retired
	Retired  int // places a planet import no longer saw (ADR-0027)
}

// ErrTooManyRetired is Import refusing to retire more than maxRetiredShare of the live places.
var ErrTooManyRetired = errors.New("spots: the file is missing too many places to be the whole planet")

// Import upserts every place in r into spots and, when planet says r is the whole planet,
// retires the places it didn't have (ADR-0027). Then, if any place was added, changed, brought
// back or retired, it bumps every account's tile version, since every account's spots tiles just
// changed; a refresh that changed nothing leaves the clients' cached tiles alone.
//
// r is a GeoJSON Text Sequence as `osmium export -f geojsonseq -u type_id` writes it (the
// recipe is docs/DEPLOY.md's): one feature per line, its OSM tags as properties, its id "n123" for a node or "a246" for an
// area (parseOSMID). Upserted by the OSM object that id stands for, so re-running it on the same
// or a newer extract updates the places already there rather than duplicating them. Each upsert
// stamps the place with the run and clears its retired_at, so a place back in OSM comes back.
//
// A place is never deleted: its captures refer to it. Retiring needs planet, since a regional
// file would otherwise retire the rest of the world, and stops with ErrTooManyRetired, retiring
// nothing, past maxRetiredShare of the live places.
//
// Registered as the `import-spots` subcommand (cmd/holdmytrack/main.go).
func Import(ctx context.Context, db DB, log *slog.Logger, r io.Reader, planet bool) (ImportStats, error) {
	return importPlaces(ctx, db, log, r, planet, maxRetiredShare)
}

func importPlaces(ctx context.Context, db DB, log *slog.Logger, r io.Reader, planet bool, maxRetired float64) (ImportStats, error) {
	var stats ImportStats
	// The run's stamp, from the database's clock: what each upsert writes into last_seen_import
	// and what retiring compares it against. clock_timestamp, not now(), which inside a
	// transaction is the transaction's start and would give two runs in one the same stamp.
	var run time.Time
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&run); err != nil {
		return stats, fmt.Errorf("spots: start: %w", err)
	}
	var batch []place
	batches := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, changed, err := upsertPlaces(ctx, db, batch, run)
		if err != nil {
			return err
		}
		stats.Imported += n
		stats.Changed += changed
		stats.Skipped += len(batch) - n
		batch = batch[:0]
		if batches++; batches%100 == 0 {
			log.Info("import-spots: progress", "imported", stats.Imported, "skipped", stats.Skipped)
		}
		return nil
	}

	br := bufio.NewReaderSize(r, 1<<20)
	for line := 1; ; line++ {
		raw, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			p, ok, perr := parseFeature(raw)
			if perr != nil {
				return stats, fmt.Errorf("spots: line %d: %w", line, perr)
			}
			if ok {
				batch = append(batch, p)
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
			return stats, fmt.Errorf("spots: read: %w", err)
		}
	}
	if err := flush(); err != nil {
		return stats, err
	}

	var retireErr error
	if planet {
		stats.Retired, retireErr = retireUnseen(ctx, db, run, maxRetired)
		if retireErr != nil && !errors.Is(retireErr, ErrTooManyRetired) {
			return stats, retireErr
		}
	}
	// Bumped even when retiring was refused: the places the run did change are already written.
	if stats.Changed+stats.Retired > 0 {
		if _, err := db.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
			return stats, fmt.Errorf("spots: bump tile versions: %w", err)
		}
	}
	return stats, retireErr
}

// retireUnseen retires the live places the run didn't stamp, and reports how many — unless that's
// more than maxRetired of them, when it retires none and returns ErrTooManyRetired.
func retireUnseen(ctx context.Context, db DB, run time.Time, maxRetired float64) (int, error) {
	var unseen, live int
	if err := db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE last_seen_import IS DISTINCT FROM $1), count(*)
		FROM spots WHERE retired_at IS NULL
	`, run).Scan(&unseen, &live); err != nil {
		return 0, fmt.Errorf("spots: count unseen places: %w", err)
	}
	if float64(unseen) > maxRetired*float64(live) {
		return 0, fmt.Errorf("%w: it would retire %d of %d live places, more than %g%%",
			ErrTooManyRetired, unseen, live, maxRetired*100)
	}
	tag, err := db.Exec(ctx, `
		UPDATE spots SET retired_at = $1
		WHERE retired_at IS NULL AND last_seen_import IS DISTINCT FROM $1
	`, run)
	if err != nil {
		return 0, fmt.Errorf("spots: retire unseen places: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// place is one feature, ready to insert. The text fields are "" when OSM has none.
type place struct {
	category, name, address string
	text                    Text
	geometry                string // GeoJSON
	osmType                 string
	osmID                   int64
}

// Text is what OSM says about a place, each field as tagged, trimmed: `description`, a
// memorial's `inscription` and `memorial` type, `start_date`, and `wikipedia` ("lang:Title").
type Text struct {
	Description, Inscription, Memorial, StartDate, Wikipedia string
}

// PlaceText reads a place's Text from its tags. A `wikipedia` value that isn't "lang:Title" —
// a bare title, or a full URL — is left out: the popup builds its link from the language.
func PlaceText(tags map[string]string) Text {
	t := Text{
		Description: strings.TrimSpace(tags["description"]),
		Inscription: strings.TrimSpace(tags["inscription"]),
		Memorial:    strings.TrimSpace(tags["memorial"]),
		StartDate:   strings.TrimSpace(tags["start_date"]),
	}
	if w := strings.TrimSpace(tags["wikipedia"]); wikipediaTag.MatchString(w) {
		t.Wikipedia = w
	}
	return t
}

// wikipediaTag is OSM's `wikipedia=lang:Title` form: a language code, a colon, a title.
var wikipediaTag = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]+)?:[^:/][^/]*$`)

type feature struct {
	ID         any             `json:"id"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties map[string]any  `json:"properties"`
}

// parseFeature reads one line. ok is false for a feature Import skips: none of the five
// categories' tags, no OSM id, or no geometry. A line that isn't JSON at all is an error — the
// file isn't what the recipe makes.
func parseFeature(raw []byte) (place, bool, error) {
	// RFC 8142 starts each record with a record separator; osmium writes it by default.
	raw = bytes.TrimLeft(raw, "\x1e \t\r\n")
	var f feature
	if err := json.Unmarshal(raw, &f); err != nil {
		return place{}, false, err
	}
	tags := make(map[string]string, len(f.Properties))
	for k, v := range f.Properties {
		if s, ok := v.(string); ok {
			tags[k] = s
		}
	}
	category := Category(tags)
	osmType, osmID, idOK := parseOSMID(f.ID)
	if !idOK {
		// `--attributes=type,id` puts them in the properties instead of the feature id.
		osmType, osmID, idOK = parseOSMAttributes(f.Properties)
	}
	if category == "" || !idOK || len(f.Geometry) == 0 || string(f.Geometry) == "null" {
		return place{}, false, nil
	}
	return place{
		category: category,
		name:     strings.TrimSpace(tags["name"]),
		address:  Address(tags),
		text:     PlaceText(tags),
		geometry: string(f.Geometry),
		osmType:  osmType,
		osmID:    osmID,
	}, true, nil
}

// Category maps a place's OSM tags to its Spots category (ADR-0021), or "" for none of them.
// A place carrying two categories' tags (a castle ruin that's also a viewpoint) gets the first
// in this order — the more specific, rarer kind of place.
func Category(tags map[string]string) string {
	switch tags["historic"] {
	case "castle", "ruins", "fort", "archaeological_site":
		return "history"
	case "monument", "memorial":
		return "monument"
	}
	if tags["tourism"] == "viewpoint" {
		return "viewpoint"
	}
	switch tags["leisure"] {
	case "dog_park":
		return "dog_park"
	case "playground":
		return "playground"
	}
	return ""
}

// Address builds a one-line postal address from a place's addr:* tags — addr:full when it has
// one, otherwise "12 Main Street, Springfield 12345". Without a street (or addr:place) there is
// no address a navigator could find, and Copy address falls back to coordinates.
func Address(tags map[string]string) string {
	if full := strings.TrimSpace(tags["addr:full"]); full != "" {
		return full
	}
	street := strings.TrimSpace(tags["addr:street"])
	if street == "" {
		street = strings.TrimSpace(tags["addr:place"])
	}
	if street == "" {
		return ""
	}
	parts := []string{join(" ", tags["addr:housenumber"], street)}
	if locality := join(" ", tags["addr:city"], tags["addr:postcode"]); locality != "" {
		parts = append(parts, locality)
	}
	return strings.Join(parts, ", ")
}

func join(sep string, words ...string) string {
	var out []string
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return strings.Join(out, sep)
}

var osmTypes = map[byte]string{'n': "node", 'w': "way", 'r': "relation"}

// parseOSMID reads osmium's type_id feature id: "n123" for a node, and "a246" for an area. An
// area's number is osmium's area id — its way's id times two, or its relation's times two plus
// one — so it maps back to the OSM object it was built from. "w123" and "r123" (a way or a
// relation exported as itself) are read too.
func parseOSMID(id any) (string, int64, bool) {
	s, ok := id.(string)
	if !ok || len(s) < 2 {
		return "", 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	if s[0] == 'a' {
		if n%2 == 0 {
			return "way", n / 2, true
		}
		return "relation", n / 2, true
	}
	typ, ok := osmTypes[s[0]]
	if !ok {
		return "", 0, false
	}
	return typ, n, true
}

func parseOSMAttributes(props map[string]any) (string, int64, bool) {
	typ, _ := props["@type"].(string)
	id, _ := props["@id"].(float64)
	if (typ != "node" && typ != "way" && typ != "relation") || id <= 0 {
		return "", 0, false
	}
	return typ, int64(id), true
}

// pointRadiusM is the radius of the circle standing in for the area of a place OSM maps as a
// single point: roughly a small playground or a memorial with the ground around it.
const pointRadiusM = 30

// upsertPlaces inserts one batch, stamping each place with run, and reports how many rows it
// wrote and how many of those changed: a place new, back from retired, or different in any
// column from what was stored. The geometry becomes the spot's area: an outline as it is (made
// valid, polygons only), anything else — a point, or the odd place mapped as a line — a
// pointRadiusM circle around a point on it. A feature whose outline doesn't survive ST_MakeValid
// is dropped. DISTINCT ON because one batch can't upsert the same row twice. `changed` reads
// spots as they were before the statement: every part of one statement sees the same snapshot.
func upsertPlaces(ctx context.Context, db DB, batch []place, run time.Time) (written, changed int, err error) {
	n := len(batch)
	categories, names, addresses := make([]string, n), make([]string, n), make([]string, n)
	descriptions, inscriptions, memorials := make([]string, n), make([]string, n), make([]string, n)
	startDates, wikipedias := make([]string, n), make([]string, n)
	geometries, osmTypes, osmIDs := make([]string, n), make([]string, n), make([]int64, n)
	for i, p := range batch {
		categories[i], names[i], addresses[i] = p.category, p.name, p.address
		descriptions[i], inscriptions[i], memorials[i] = p.text.Description, p.text.Inscription, p.text.Memorial
		startDates[i], wikipedias[i] = p.text.StartDate, p.text.Wikipedia
		geometries[i], osmTypes[i], osmIDs[i] = p.geometry, p.osmType, p.osmID
	}
	err = db.QueryRow(ctx, `
		WITH incoming AS (
			SELECT DISTINCT ON (osm_type, osm_id) *
			FROM (
				SELECT r.category, NULLIF(r.name, '') AS name, NULLIF(r.address, '') AS address,
				       NULLIF(r.description, '') AS description, NULLIF(r.inscription, '') AS inscription,
				       NULLIF(r.memorial, '') AS memorial, NULLIF(r.start_date, '') AS start_date,
				       NULLIF(r.wikipedia, '') AS wikipedia,
				       r.osm_type, r.osm_id,
				       CASE WHEN ST_Dimension(g) = 2
				            THEN ST_Multi(ST_CollectionExtract(ST_MakeValid(g), 3))
				            ELSE ST_Multi(ST_Buffer(ST_PointOnSurface(g)::geography, $12)::geometry)
				       END AS area
				FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[],
				            $7::text[], $8::text[], $9::text[], $10::text[], $11::bigint[])
				         AS r(category, name, address, description, inscription, memorial, start_date,
				              wikipedia, geojson, osm_type, osm_id)
				CROSS JOIN LATERAL (SELECT ST_SetSRID(ST_GeomFromGeoJSON(r.geojson), 4326) AS g) geo
			) rows
			WHERE NOT ST_IsEmpty(area)
			ORDER BY osm_type, osm_id
		),
		changed AS (
			SELECT count(*) AS n
			FROM incoming i
			LEFT JOIN spots s ON s.osm_type = i.osm_type AND s.osm_id = i.osm_id
			WHERE s.id IS NULL OR s.retired_at IS NOT NULL
			   OR (s.category, s.name, s.address, s.description, s.inscription, s.memorial,
			       s.start_date, s.wikipedia, s.geom)
			      IS DISTINCT FROM
			      (i.category, i.name, i.address, i.description, i.inscription, i.memorial,
			       i.start_date, i.wikipedia, i.area)
		),
		written AS (
			INSERT INTO spots (category, name, address, description, inscription, memorial, start_date,
			                   wikipedia, geom, osm_type, osm_id, last_seen_import)
			SELECT category, name, address, description, inscription, memorial, start_date,
			       wikipedia, area, osm_type, osm_id, $13
			FROM incoming
			ON CONFLICT (osm_type, osm_id) DO UPDATE SET
				category = EXCLUDED.category, name = EXCLUDED.name, address = EXCLUDED.address,
				description = EXCLUDED.description, inscription = EXCLUDED.inscription,
				memorial = EXCLUDED.memorial, start_date = EXCLUDED.start_date,
				wikipedia = EXCLUDED.wikipedia, geom = EXCLUDED.geom,
				last_seen_import = EXCLUDED.last_seen_import, retired_at = NULL
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM written), (SELECT n FROM changed)
	`, categories, names, addresses, descriptions, inscriptions, memorials, startDates, wikipedias,
		geometries, osmTypes, osmIDs, float64(pointRadiusM), run).Scan(&written, &changed)
	if err != nil {
		return 0, 0, fmt.Errorf("spots: upsert places: %w", err)
	}
	return written, changed, nil
}
