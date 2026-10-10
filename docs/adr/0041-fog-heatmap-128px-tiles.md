# ADR-0041: Fog and Heatmap tiles are 128 px

## Status

Accepted. Built on the server (`fog.TileSize`, `strokeRadiusPx`, `IMPLEMENTATION.md` §4.2); the web and Android clients are unchanged. It supersedes ADR-0037's tile size, 64 px, and keeps its approach: one size for masks and served tiles, drawn at 512 by the clients. ADR-0038's square pixels below zoom 14 stay, at about 4 screen pixels rather than 8.

## Context

ADR-0037 made every Fog and Heatmap tile, stored mask and per-activity mask 64 px, about 38 m a pixel at z14, to cut the render's pixel work and the account's storage. Seen from zoom 13 out, 64 px smeared thin tracks: routes a few hundred metres apart merged into one chunky band, a diagonal road became a coarse staircase, and the Heatmap's lines thickened into saturated blocks that hid where tracks overlapped.

Two things then changed what a bigger tile costs. Activity masks moved into Postgres (ADR-0040), so a render no longer pays a round trip to object storage per mask, and its time is mostly the tiles' own PUTs and the pyramid's GETs, which don't depend on the image size. And production showed the worker waiting rather than computing (`PERFORMANCE.md`, the 2026-10-09 production session).

## Decision

**Fog and Heatmap tiles, their pyramid and every per-activity mask are 128×128** (`fog.TileSize`), about 19 m a pixel at z14. The stroke doubles in pixels (`strokeRadiusPx` = 2.5) to keep its ~48 m band either side, and `TileMarginPx` follows it. `minStepPx` stays half a pixel: the jitter it guards against is about pixels, not metres, and 300 random jittery walks all draw at 128 px.

Chosen on the dev stack by comparing the demo tracks at 64 and 128 px, Fog and Heatmap, at zoom 10, 13 and 16, light and dark (`PERFORMANCE.md`, 2026-10-09). At 128 px, tracks stay apart at zoom 10, follow their roads at zoom 13, and the Heatmap reads as lines whose overlaps glow. At zoom 16 the band looks the same at both sizes.

## Alternatives considered

- **Stay at 64 px.** Rejected for the look above, now that its saving in render time is mostly gone.
- **256 or 512 px.** Rejected for now: 512 px was compared in ADR-0037 and looked hardly different at the zooms people browse at, for 16 times 128 px's pixels and several times its storage. 256 px would double the masks' bytes in Postgres again for detail finer than a GPS track holds.
- **A separate size for masks and served tiles.** Rejected, as in ADR-0037: one size keeps the composite a plain per-pixel max or sum with no resampling.

## Consequences

- Each per-tile step costs 2–5 times what it did at 64 px, still microseconds next to a round trip: compositing a Fog tile 27 µs (7 µs), a Heatmap tile 65 µs (13 µs), a pyramid step 31 µs (8 µs), decoding a mask 38 µs (15 µs), encoding one 188 µs (135 µs) (`tilecost_bench_test.go`, on a development laptop).
- A mask averages about 600 bytes rather than 275, so `activity_tile_mask_data` holds about 5 KB per activity (ADR-0040).
- Every stored mask had to be redrawn, by the same `rerender-coverage --masks` that moved the masks into Postgres, on one deploy.
- ADR-0038's squares are half the size: a far-off track is a thinner line of smaller pixels.
