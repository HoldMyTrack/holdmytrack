// Package geo backs the Country/Region zoom tiers Fog and Heatmap fall back to below city
// zoom (docs/IMPLEMENTATION.md §4.2.4): "have you been anywhere in this country/region at
// all," as a whole-polygon reveal, distinct from the per-pixel raster pyramid internal/fog
// owns and which stays exactly as it is above that threshold.
//
// admin_countries/admin_regions (migrations/0020_admin_boundaries.sql) hold Natural Earth
// country and admin-1 (state/province) polygons, vendored under seed-data/ and loaded once by
// SeedAdminBoundaries. activity_country/activity_region record, once per activity, which of
// those polygons its trajectory touches — computed by MatchActivity, called from
// internal/ingest right after an activity is persisted — so the "is this country unlocked"
// tile queries (internal/httpapi's admin_country_tiles.go/admin_region_tiles.go) only ever do
// a cheap indexed EXISTS/NOT EXISTS lookup, never the geometry test itself.
package geo

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed seed-data/*.geojson
var seedData embed.FS

// Natural Earth's own 1:50m Admin-0 (country) and 1:10m Admin-1 (state/province) exports,
// trimmed to the properties this package reads and coordinate-simplified for their role here
// (a whole-polygon fill that only ever renders below z8 — see docs/adr/0008 for why 1:10m,
// not 1:50m, for regions specifically: the 1:50m Admin-1 export only covers 9 of 242
// countries, whereas 1:10m covers every one of them).
const (
	countriesFile = "seed-data/ne_50m_admin_0_countries.geojson"
	regionsFile   = "seed-data/ne_10m_admin_1_states_provinces.geojson"
)

type featureCollection struct {
	Features []feature `json:"features"`
}

type feature struct {
	Properties json.RawMessage `json:"properties"`
	Geometry   json.RawMessage `json:"geometry"`
}

type countryProps struct {
	Name    string `json:"NAME"`
	Adm0A3  string `json:"ADM0_A3"`
	IsoA2   string `json:"ISO_A2"`
	IsoA2EH string `json:"ISO_A2_EH"`
}

type regionProps struct {
	Name     string `json:"name"`
	Adm0A3   string `json:"adm0_a3"`
	Iso31662 string `json:"iso_3166_2"`
	Adm1Code string `json:"adm1_code"`
}

func readFeatures(path string) ([]feature, error) {
	raw, err := seedData.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("geo: read %s: %w", path, err)
	}
	var fc featureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("geo: parse %s: %w", path, err)
	}
	return fc.Features, nil
}

// nullableISO reports Natural Earth's "-99" sentinel and the empty string as "no code," and
// otherwise the code as-is.
func nullableISO(code string) *string {
	if code == "" || code == "-99" {
		return nil
	}
	return &code
}
