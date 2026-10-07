# ADR-0034: Country and region outlines come from Overture Maps' divisions, land-clipped, loaded from our own copy at deploy time

## Status

Accepted. Built in `internal/geo` and `scripts/boundaries-extract.sh` (`IMPLEMENTATION.md` §3.12–§3.13, §4.2.4).

## Context

Fog's and Heatmap's Country/Region tiers unlock a country or region the moment an activity touches it (`SPEC.md` FR-4.2, FR-4.3), so the outlines decide which places a person has been. Natural Earth's 1:50m countries and 1:10m states/provinces, simplified further to embed in the server, put those borders up to a kilometre or more off. A walk at Niagara Falls, NY unlocked Canada and Ontario; a walk along the seafront at Ostia matched no country, its whole track out at sea by Natural Earth's coastline; a walk in St Peter's Square showed only Italy, Vatican City's outline sitting 1.6 km west of the real one. Anyone who lives or travels near a border or a coast gets the wrong answer, and a wrong "you've been to Canada" is the one thing this feature can't afford.

## Decision

**Overture Maps' divisions theme**: its countries, dependencies and regions, which are OpenStreetMap's administrative borders assembled into polygons, under ODbL. Concretely:

- **Land-clipped outlines, for matching and drawing alike.** Overture has two outlines for a coastal unit: one clipped to the land, and one that runs out to the edge of its territorial sea. The land one keeps the fills looking like the map underneath and the data smaller; the demo's walk along the seafront at Ostia, which Natural Earth put at sea, matches Italy on it, since OpenStreetMap's coastline runs along the water's edge.
- **One pinned release, cut by a public script, served from our own bucket.** `scripts/boundaries-extract.sh` cuts the world's land outlines out of one Overture release with DuckDB, off the server; the result, about 500 MB gzipped, goes to the app bucket by hand, and `seed-admin-boundaries` reads it from there. Overture deletes a release a few months after the next one, so a deployment can't depend on Overture's bucket, and the file is far too large for the repository. The script is also the public record of how the derived outlines were made, which is what ODbL's share-alike asks of a derived database.
- **Full detail for matching, simplified for drawing.** Matching tests the full outlines, cut into small pieces so a track crossing a long coast is an index lookup; the tiles, drawn on every request (ADR-0008), read a copy simplified to under a pixel at the zooms each tier draws (about 5 km for countries, 1 km for regions), so they cost what Natural Earth's did.
- **Overture's default view of disputed areas**, unchanged: it draws a disputed area as its own unit where OpenStreetMap does (Western Sahara, Aksai Chin). Where Overture draws one region twice, once with an ISO code and once without, the one without is dropped.
- **Credit**: the map already credits OpenStreetMap for the basemap; the Country/Region sources add Overture Maps.

## Alternatives considered

- **Natural Earth 1:10m countries, unsimplified.** Public domain and a few megabytes, so it could stay embedded. Rejected: at 1:10 million it still put Niagara Falls in Canada and St Peter's Square in Italy.
- **geoBoundaries.** Rejected: each country's file comes from a different source under a different licence (a third of them ODbL anyway, others CC BY-SA, CC BY 3.0 IGO and more), accuracy ranges from OpenStreetMap's to a sketch from Wikipedia, most sources date from 2017, and a country's ADM0 and ADM1 often come from different sources and disagree at the border. Its composite, CGAZ, is heavily simplified.
- **GADM.** Rejected: its licence forbids redistribution and commercial use.
- **Cutting the outlines from the OpenStreetMap planet ourselves** (`admin_level` 2 and 4 with osmium). Rejected: an 85 GB download, multipolygon assembly and coastline clipping of our own, and `admin_level=4` means something different in each country, all of which Overture has already done.
- **The territorial-sea outlines.** They would count a kayak or a ferry a few kilometres out as being in the country. Rejected for now: the land outlines already match the shoreline walks that failed, at a smaller size, and the fills follow the coast.
- **Embedding the extract in the server image, or committing it.** Rejected: hundreds of megabytes in every image and every clone, and a committed copy would be a distribution of the database itself, carrying an ODbL notice of its own.

## Consequences

- A deploy that brings a new extract runs `seed-admin-boundaries` once after uploading it (`DEPLOY.md` §6). The seed replaces every outline and re-matches every activity in one transaction, so it takes minutes and blocks ingest's country matching while it runs; it's done with the site in maintenance mode. Re-running it with the extract already loaded does nothing.
- Every development or new deployment needs the extract made or copied in before the tiers draw anything; until then they're blank, as before any seed.
- Moving to a newer Overture release is a deliberate step: change `RELEASE` in the script and `geo.BoundariesKey`, make and check the extract, upload it, seed. OpenStreetMap edits between releases can move a border, and a reload re-matches every activity against the new outlines.
- The tiles' outlines are simplified one polygon at a time, so neighbours can show hairline gaps or overlaps at the lowest zooms. Simplifying shared edges together (`ST_CoverageSimplify`) needs GEOS 3.12, newer than the database image's 3.9.