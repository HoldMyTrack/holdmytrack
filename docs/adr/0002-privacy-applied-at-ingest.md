# ADR-0002: Privacy is applied at ingest, never at render

## Status

Accepted.

## Context

A Fog of War map is, by construction, a precise record of where a person lives and when they're away from home — Strava's 2018 global heatmap incident is the canonical example of exactly this kind of data leaking in a way its own users never expected. FitMap's core privacy controls (endpoint trimming, user-defined privacy zones) exist specifically to prevent that, by removing the sensitive points — the start/end of a track, or any point inside a zone — before a route is ever a fog reveal, a track line, or an export.

The obvious place to apply that removal is wherever the map is actually drawn: filter the points at render time, right before they become pixels. That would also be the most flexible point, since a user could add a privacy zone and see it applied immediately everywhere, with no reprocessing.

## Decision

**Privacy is enforced once, at ingest** (`IMPLEMENTATION.md` §4.1 step 3) — the raw point list is clipped against `privacy_zones` and trimmed by `privacy_trim_m` from each end *before anything is persisted, indexed, or rendered*. Nothing downstream — the fog raster pipeline, the tracks tile query, colored zone segments, the high-resolution export — ever touches a point this step has already removed, because that point was never stored anywhere those systems read from.

Adding a privacy zone after the fact means re-processing already-ingested activities: a `reprivacy` job re-parses each affected activity's retained raw payload and re-runs ingest steps 2–6 against the new zone set, marking affected fog tiles dirty — the same code path as initial ingest, not a bespoke re-clip (`IMPLEMENTATION.md` §7). This is also why raw (unclipped) payloads are retained per source at all, rather than discarded after first parse: retroactive re-clipping needs the server to hold points it can still reclip.

## Alternatives considered

- **Filter at render time.** Rejected — this was the default, most flexible-seeming option, and specifically the one rejected. A render-time filter means every current and future rendering path (the fog rasterizer, the tile server, colored zone segments, the exporter, a future print pipeline) has to correctly re-implement the same exclusion logic, forever. One bug in any of those paths — not just today's, any future one — leaks a point ingest-time filtering would have already removed permanently. Privacy became a property of the data itself, not a property every consumer of the data has to remember to uphold.
- **Filter client-side, before upload.** Rejected for the same retroactive-re-clipping reason above: if trimming happened before the server ever saw the raw points, adding a privacy zone later would have nothing left to reclip against, since the server would have never retained the now-sensitive points at all.

## Consequences

- Every current and future renderer (fog, tracks, colored bands, export, and any print pipeline built later) inherits correct privacy behavior automatically, with nothing extra to implement or to get wrong.
- Raw, unclipped payloads have to be retained server-side to make retroactive re-clipping possible at all — which makes them, by FitMap's own account, "the most sensitive artifact in the system" (`IMPLEMENTATION.md` §7), requiring encryption at rest and aggressive, scheduled expiry (§5.7). An activity whose raw payload has already expired by the time a new privacy zone is added simply can't be retroactively re-clipped — an accepted limit of the retention window, not a bug.
- Adding a privacy zone is not instant for existing history — it requires a real background job over every activity that zone affects, not an immediate render-time change.
- This decision is exactly why the no-signup demo account (ADR-0004) could safely reuse the real ingest pipeline: privacy enforcement is identical for a demo account and a real one, with nothing demo-specific to get wrong.