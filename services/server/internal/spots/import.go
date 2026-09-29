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

// Import upserts every place in r into spots, then queues the `match_spots` backfill job that
// matches every existing activity against them (BackfillJob), and bumps every account's tile
// version, since every account's spots tiles just changed.
//
// r is a GeoJSON Text Sequence as `osmium export -f geojsonseq -u type_id` writes it (the
// recipe is docs/DEPLOY.md's): one feature per line, its OSM tags as properties, its id "n123" for a
// node or "a246" for an area (parseOSMID). Upserted by the OSM object that id stands for, so re-running it on the same or a newer extract
// updates the places already there rather than duplicating them.
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

	if err := EnqueueBackfill(ctx, pool); err != nil {
		return stats, fmt.Errorf("spots: enqueue backfill: %w", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET map_version = map_version + 1`); err != nil {
		return stats, fmt.Errorf("spots: bump tile versions: %w", err)
	}
	return stats, nil
}

// BackfillJob is the `match_spots` job's payload: nothing, since it matches every activity.
type BackfillJob struct{}

// EnqueueBackfill queues one `match_spots` job, unless one is already waiting to start — a
// second import landing before the first backfill ran needs no second pass.
func EnqueueBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO jobs (kind, payload)
		SELECT 'match_spots', '{}'::jsonb
		WHERE NOT EXISTS (
			SELECT 1 FROM jobs WHERE kind = 'match_spots' AND state = 'pending' AND locked_at IS NULL
		)
	`)
	return err
}

// place is one feature, ready to insert.
type place struct {
	category, name, address string
	geometry                string // GeoJSON
	osmType                 string
	osmID                   int64
}

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

// upsertPlaces inserts one batch and reports how many rows it wrote. The geometry becomes the
// spot's area: an outline as it is (made valid, polygons only), anything else — a point, or the
// odd place mapped as a line — a 50 m circle around a point on it. A feature whose outline
// doesn't survive ST_MakeValid is dropped. DISTINCT ON because one batch can't upsert the same
// row twice.
func upsertPlaces(ctx context.Context, pool *pgxpool.Pool, batch []place) (int, error) {
	categories := make([]string, len(batch))
	names := make([]string, len(batch))
	addresses := make([]string, len(batch))
	geometries := make([]string, len(batch))
	osmTypes := make([]string, len(batch))
	osmIDs := make([]int64, len(batch))
	for i, p := range batch {
		categories[i], names[i], addresses[i] = p.category, p.name, p.address
		geometries[i], osmTypes[i], osmIDs[i] = p.geometry, p.osmType, p.osmID
	}
	tag, err := pool.Exec(ctx, `
		INSERT INTO spots (category, name, address, geom, osm_type, osm_id)
		SELECT DISTINCT ON (osm_type, osm_id) category, name, address, area, osm_type, osm_id
		FROM (
			SELECT r.category, NULLIF(r.name, '') AS name, NULLIF(r.address, '') AS address,
			       r.osm_type, r.osm_id,
			       CASE WHEN ST_Dimension(g) = 2
			            THEN ST_Multi(ST_CollectionExtract(ST_MakeValid(g), 3))
			            ELSE ST_Multi(ST_Buffer(ST_PointOnSurface(g)::geography, 50)::geometry)
			       END AS area
			FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::bigint[])
			         AS r(category, name, address, geojson, osm_type, osm_id)
			CROSS JOIN LATERAL (SELECT ST_SetSRID(ST_GeomFromGeoJSON(r.geojson), 4326) AS g) geo
		) rows
		WHERE NOT ST_IsEmpty(area)
		ORDER BY osm_type, osm_id
		ON CONFLICT (osm_type, osm_id) DO UPDATE SET
			category = EXCLUDED.category, name = EXCLUDED.name,
			address = EXCLUDED.address, geom = EXCLUDED.geom
	`, categories, names, addresses, geometries, osmTypes, osmIDs)
	if err != nil {
		return 0, fmt.Errorf("spots: upsert places: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
