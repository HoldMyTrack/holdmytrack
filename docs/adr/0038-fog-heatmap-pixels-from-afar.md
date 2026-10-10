# ADR-0038: Fog and Heatmap show square pixels from afar and are smoothed from zoom 14

## Status

Accepted. Built in the web client and the Android app (`IMPLEMENTATION.md` §4.2; `SPEC.md` FR-4.2). It changes how ADR-0037's 64 px tiles are drawn below zoom 14; the tiles themselves are unchanged. Since ADR-0041 the tiles are 128 px, so a square is about 4 screen pixels rather than 8.

## Context

ADR-0037 made Fog and Heatmap tiles 64 px and had the clients stretch them smoothly at every zoom. Seen on the dev stack, the smooth look read well close up but blurred everything from a distance, where coverage is mostly thin lines across a city or a region. Both clients draw a tile 512 screen pixels wide, so below z14 a tile pixel is always about 8 screen pixels however far out the map is, while from z14 in there is no finer tile and the z14 one is stretched further with every zoom — 16 screen pixels at z15, 32 at z16, 64 at z17.

## Decision

**Below zoom 14 the clients draw Fog and Heatmap pixels as squares; from zoom 14 in they smooth them.** One zoom-stepped `raster-resampling` (`nearest` below `PIXELS_MAX_ZOOM` = 14, `linear` from it) on each raster layer, in `apps/web/src/map/fog.ts` and `heatmap.ts` and Android's `MapOverlays`. The switch is instant as the zoom crosses 14: MapLibre steps this property, it doesn't blend it.

## Alternatives considered

- **Smooth at every zoom** (ADR-0037 as built). Soft everywhere; from a distance tracks lose their edge.
- **Square pixels at every zoom.** Crisp from afar, but close up a pixel grows into a block as wide as a street and then a city block.
- **Square pixels close up, smooth from afar.** Tried first on dev; the large blocks were exactly where pixels look worst.
- **Hard pixels drawn on the server** (no anti-aliasing in the masks). Would also remove the partly transparent edge pixels, but needs a re-render and gains little once the client draws them square.

## Consequences

- Client-only: no server change and no re-render. Both clients need it; an older Android build keeps the smooth look until updated.
- The edge pixels of a stroke stay partly transparent (the masks are anti-aliased), so a pixel line has a softer fringe of lighter squares.
- The high-resolution export captures whichever look the zoom it's taken at shows.