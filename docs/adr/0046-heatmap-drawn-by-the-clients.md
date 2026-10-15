# ADR-0046: The Heatmap is pass counts on the server, drawn by the clients as MapLibre heatmap circles

## Status

Accepted. Built: each z14 tile's Heatmap is stored as per-pixel pass counts, served as weighted points (`GET /tiles/v1/heat-points/…`) and drawn on the web and in the Android app as a MapLibre `heatmap` layer at every zoom (`IMPLEMENTATION.md` §4.2.2, `SPEC.md` FR-4.3).

## Context

The Heatmap was a server-rendered raster: per z14 tile, the summed strokes of the activities in the window, scaled against the account's `heatmap_cap` and coloured as a palette when served, with flat country and region fills below zoom 7. It read as hard red lines: one pass drew the same deep red band as Fog's stroke, the pyramid kept each block's hottest pixel, and zoomed out it switched to whole countries filled the same colour whether visited once or daily. What people expect of a heatmap is soft heat that warms where they go often and fades at its edges, and blobs across the globe when zoomed out.

A softer raster was built first and measured: the stroke narrowed, blurred into a core and a halo, a log scale, a summing pyramid. It looked right, but the blur reaches past a tile, so every z14 render had to read its eight neighbours' masks and every change had to re-render the tiles around it; it cost a fifth more render time and a third more storage, was baked into every stored tile so any change to the look meant a full re-render, and still went blurry past zoom 14, where nothing finer is stored.

## Decision

- **The server stores counts, not a picture**: per pixel, how many in-window activities' strokes (narrowed by a pixel) cover it, in a 16-bit PNG per tile on the same `fog_tiles` row and render as Fog; the pyramid keeps each block's busiest pixel.
- **A heat-points tile is made per request**: a point per non-empty cell, ~8 px across as drawn, counting the activities through it up to zoom 8 (from the activity rows) and its busiest pixel from zoom 9 (from the count tile one zoom up), weighted on a log scale against the zoom's busiest cell and divided by how much its neighbours' circles overlap it.
- **The clients draw them** as MapLibre heatmap circles, at every zoom, with the same radius, intensity and colour ramp on the web and in the Android app.
- **The old routes stay for builds already installed**: the raster route answers a transparent tile, and the Country/Region Heatmap tiles keep working, until those builds are gone (`ROADMAP.md`).

## Alternatives considered

- **The soft raster above.** Rejected for its render cost, its neighbour reads, a look baked into storage, and blur past zoom 14.
- **The soft raster close in, circles only zoomed out.** Keeps all of the raster's machinery to save a handful of zooms, and adds a visible handoff between two ways of drawing.
- **One way of scoring a cell at every zoom.** The busiest pixel everywhere made a road driven twice read nearly as hot as a city walked twenty different ways, once zoomed out; the activities through a cell everywhere needs, for cells finer than a z14 tile, data the per-pixel counts can't give.
- **An intensity that changes with zoom** instead of weighting each point for its neighbours. Lone places and routes want different intensities at the same zoom, so any curve left one too faint or the other too hot.
- **Shipping points for every pixel, or a GeoJSON per account.** A dense account is hundreds of thousands of points at street level; tiles keep a request to at most 4,096.

## Consequences

- **Looks change without a re-render**, and so does a new `heatmap_cap`: the clients' paint and the per-request weighting carry both. The cap sweep only bumps the tile version.
- **Renders and uploads cost what they did** (`PERFORMANCE.md`, 2026-10-15); a tile served costs a row, an object read and a decode, the read counted against the R2 budget (ADR-0044), and is bigger on the wire than the raster's PNG, up to ~50 KB over a dense area.
- **MapLibre adds overlapping circles**, so the server has to know the clients' circle radius to weight points against it: the two must change together. A point's neighbours in the next tile aren't seen, so the edge of a tile can read a touch hot.
- **Android builds from before this show no heat in Heatmap mode** until updated; their Country/Region tiers still draw.
- **The stored Heatmap objects changed format**: deploying it takes one `rerender-coverage`, until which a tile still in the old raster serves as empty.