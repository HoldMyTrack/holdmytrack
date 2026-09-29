// Package spots is Spots (IMPLEMENTATION.md §3.20, §4.25, ADR-0021): outdoor places imported
// once from OpenStreetMap, shown on the map behind the Show POI toggle.
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

	"github.com/jackc/pgx/v5/pgxpool"
)

// importBatch is how many places one INSERT carries. Big enough that a planet-sized import
// isn't dominated by round trips, small enough that one batch's geometries stay a few MB.
const importBatch = 1000

// ImportStats is what Import reports back for the log.
type ImportStats struct {
	Imported int // upserted into spots
	Skipped  int // features with no category, no usable id or no geometry
}

// Import upserts every place in r into spots, then bumps every account's tile version, since
// every account's spots tiles just changed.
//
// r is a GeoJSON Text Sequence as `osmium export -f geojsonseq -u type_id` writes it (the
// recipe is docs/DEPLOY.md's): one feature per line, its OSM tags as properties, its id "n123" for a node or "a246" for an
// area (parseOSMID). Upserted by the OSM object that id stands for, so re-running it on the same
// or a newer extract updates the places already there rather than duplicating them.
//
// Registered as the `import-spots` subcommand (cmd/holdmytrack/main.go).
func Import(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, r io.Reader) (ImportStats, error) {
	var stats ImportStats
	var batch []place
	batches := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := upsertPlaces(ctx, pool, batch)
		if err != nil {
			return err
		}
		stats.Imported += n
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

	if _, err := pool.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
		return stats, fmt.Errorf("spots: bump tile versions: %w", err)
	}
	return stats, nil
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

// upsertPlaces inserts one batch and reports how many rows it wrote. The geometry becomes the
// spot's area: an outline as it is (made valid, polygons only), anything else — a point, or the
// odd place mapped as a line — a pointRadiusM circle around a point on it. A feature whose outline
// doesn't survive ST_MakeValid is dropped. DISTINCT ON because one batch can't upsert the same
// row twice.
func upsertPlaces(ctx context.Context, pool *pgxpool.Pool, batch []place) (int, error) {
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
	tag, err := pool.Exec(ctx, `
		INSERT INTO spots (category, name, address, description, inscription, memorial, start_date,
		                   wikipedia, geom, osm_type, osm_id)
		SELECT DISTINCT ON (osm_type, osm_id)
		       category, name, address, description, inscription, memorial, start_date,
		       wikipedia, area, osm_type, osm_id
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
		ON CONFLICT (osm_type, osm_id) DO UPDATE SET
			category = EXCLUDED.category, name = EXCLUDED.name, address = EXCLUDED.address,
			description = EXCLUDED.description, inscription = EXCLUDED.inscription,
			memorial = EXCLUDED.memorial, start_date = EXCLUDED.start_date,
			wikipedia = EXCLUDED.wikipedia, geom = EXCLUDED.geom
	`, categories, names, addresses, descriptions, inscriptions, memorials, startDates, wikipedias,
		geometries, osmTypes, osmIDs, float64(pointRadiusM))
	if err != nil {
		return 0, fmt.Errorf("spots: upsert places: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
