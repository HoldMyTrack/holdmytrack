// Package geo backs the Country/Region zoom tiers Fog and Heatmap fall back to below city
// zoom (docs/IMPLEMENTATION.md §4.2.4): "have you been anywhere in this country/region at
// all," as a whole-polygon reveal, distinct from the per-pixel raster pyramid internal/fog
// owns and which stays exactly as it is above that threshold.
//
// admin_countries/admin_regions hold Overture Maps' land-clipped country and region outlines
// (OpenStreetMap's borders, ADR-0034), loaded by SeedAdminBoundaries from an extract made off
// the server by scripts/boundaries-extract.sh: simplified there for the tiles, and in full detail,
// cut into pieces, in admin_country_parts/admin_region_parts for matching.
// activity_country/activity_region record, once per activity, which of them its trajectory
// touches — computed by MatchActivity, called from internal/ingest right after an activity is
// persisted — so the "is this country unlocked" tile queries (internal/httpapi's
// admin_country_tiles.go/admin_region_tiles.go) only ever do a cheap indexed EXISTS/NOT EXISTS
// lookup, never the geometry test itself.
package geo

// BoundariesKey is the extract seed-admin-boundaries reads from the app bucket by default:
// scripts/boundaries-extract.sh's output for the release it pins, uploaded by hand
// (docs/DEPLOY.md §6). A newer release changes both.
const BoundariesKey = "boundaries/overture-2026-09-23.0-boundaries.csv.gz"
