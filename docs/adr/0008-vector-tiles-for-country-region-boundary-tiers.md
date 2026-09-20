# ADR-0008: Live vector tiles for the Country/Region zoom tiers, not a precomputed raster

## Status

Accepted.

## Context

Fog of War and Heatmap show true per-pixel history at every zoom, which means at country- or continent-wide zoom almost the entire visible map reads as "unexplored" even for an account with activities scattered across many countries — the raster is accurate but discouraging, and doesn't reward long-distance travel the way it should. `IMPLEMENTATION.md` §4.2.4 introduces two coarser zoom tiers below city zoom: below it, a whole country or state/region renders as a single "unlocked" or "locked" fill, with no per-pixel detail, switching over to the exact existing raster pyramid at city zoom and above.

That raster pyramid (ADR-0003) is precomputed specifically because its cost scales with a user's own activity count, composited per pixel — exactly the scaling wall a live per-request query would hit. The question this ADR answers is whether the new coarse tiers should be built the same way, as a second precomputed raster pyramid, or differently.

## Decision

**The Country/Region tiers are served as live vector tiles (MVT), generated per request, not precomputed.** `admin_countries`/`admin_regions` (`IMPLEMENTATION.md` §3.12–§3.13) hold Natural Earth's country and state/province polygons — a small, fixed row count (~250 countries, ~4,600 regions) that never grows with a user's activity history. `activity_country`/`activity_region` (§3.14–§3.15) record, once per activity at ingest time, which polygons its trajectory touches. The tile endpoints then do a single indexed `EXISTS`/`NOT EXISTS` join against that membership table per request — the expensive geometry test never runs at request time, only a cheap lookup against a fixed-size table does, so cost scales with the boundary dataset's size, not the requesting user's history.

This mirrors the Tracks endpoint's own live-MVT shape (`IMPLEMENTATION.md` §4.3), not fog/heatmap's raster pyramid — the two features share a tile-serving pattern with Tracks specifically because both have the same cost shape Tracks does (bounded, not scaling with account history), which fog/heatmap's own raster tiers do not.

## Alternatives considered

- **A second precomputed raster pyramid**, mirroring fog/heatmap exactly. Rejected: country/region edges need to read as crisp political boundaries, not soft-blurred the way fog's `featherPx` box blur treats a coverage edge; a raster tier would also need its own dirty-tile/pyramid-rebuild machinery for data that only changes when an activity is ingested or deleted — real infrastructure to build for a case that, unlike fog/heatmap, was never the scaling problem in the first place.
- **Live per-request `ST_Intersects` against `activities.trajectory` directly**, skipping the `activity_country`/`activity_region` membership tables and matching at read time instead of ingest time. Rejected: this is exactly the "cost scales with a user's own activity count" shape ADR-0003 already ruled out for fog, and the same shape the old live-heatmap-compositing path was removed for (~12.6s/tile against a dense account) — matching once at ingest, read back as a cheap lookup, is what avoids reintroducing that.
- **Baking country/region colour into the server-rendered tile**, matching how fog/heatmap bake their veil/ramp colour into a PNG because MapLibre has no `raster-color`. Not needed here: a vector fill layer's `fill-color`/`fill-opacity` are native MapLibre paint properties, so there's no equivalent constraint to work around.

## Consequences

- Reading a Country/Region tile costs one indexed join against a table whose size is fixed by the boundary dataset, not by the requesting account's history — the same property precomputation exists to guarantee for fog/heatmap, achieved here without precomputing anything.
- Deleting or re-ingesting an activity needs no dirty-marking, no rebuild worker, and no cache invalidation for this tier: `activity_country`/`activity_region` rows cascade-delete with the activity, and the next tile request simply sees a different join result. Fog/heatmap's entire dirty/rebuild pipeline (§4.2.3) has no counterpart here.
- The Country/Region tiers depend on Natural Earth boundary data being present and current; unlike fog/heatmap's coverage, which is entirely derived from a user's own uploads, an error or gap in the vendored boundary dataset (see §3.12's note on the ~16 Natural Earth region rows with no matching country) is a shared, not per-account, limitation.