# ADR-0018: No explorer tiles — Fog of War is the exploration mechanic

## Status

Accepted.

## Context

`VISION.md` promised two exploration mechanics side by side: Fog of War, and explorer tiles — the z14 (~2.4 km) and z17 (~306 m) slippy-tile squares that Statshunters, VeloViewer and Squadrats count, scored as total tiles, max square, max connected cluster and coverage % by region. Fog of War was built (`IMPLEMENTATION.md` §4.2). Explorer tiles never were: a `user_tiles` table was created in `0003_tiles.sql` and stayed empty, with nothing writing or reading it, and a Phase 4 roadmap item waited on it.

Looked at again, the two answer the same question. Both show where someone has been; the fog shows it at ~5 m per pixel with a soft reveal, the tiles as coarse squares. The fog is the friendlier of the two to look at, and the look is what this product competes on (`VISION.md` §1).

## Decision

**There is no explorer-tile scoring. Fog of War is HoldMyTrack's exploration mechanic.** The `user_tiles` table is removed from the schema, and the tile game is removed from `VISION.md`, `ROADMAP.md`, `SPEC.md` and `IMPLEMENTATION.md` (§3.5 and §4.4 are kept only as "removed" stubs so later section numbers don't shift). Nothing replaces it on the roadmap.

## Alternatives considered

- **Build the z14/z17 scoring as planned.** Rejected: a second, coarser view of what the fog already shows. It would also need bookkeeping the fog doesn't — a per-user aggregate of visited squares has to be un-visited correctly when an activity is deleted, trimmed, cropped by a new Private location or superseded as a duplicate, which a running `visit_count` cannot do.
- **Keep the empty table "just in case".** Rejected: schema nobody writes, with documentation describing a feature nobody plans to build, is a promise the docs keep making on the product's behalf.
- **Replace it with fog-derived coverage stats** (area explored, % of a country or region, computed from the fog masks). Not taken up now; it would be a new decision of its own, not part of this one.

## Consequences

- No max-square, cluster or tile-count scores. People who play the tile games (Squadrats and the rest) are not served by HoldMyTrack, and `VISION.md`'s competitor table now says so plainly.
- The fog stays the only coverage grid, so ingest, deletion, track edits and Private locations have one set of per-tile data to keep right (`activity_tile_masks`, `fog_tiles`), not two.
- `0003_tiles.sql` was edited in place rather than followed by a `DROP TABLE` migration — possible only because the sandbox had two users; its existing database was patched by hand (`IMPLEMENTATION.md`'s schema note).