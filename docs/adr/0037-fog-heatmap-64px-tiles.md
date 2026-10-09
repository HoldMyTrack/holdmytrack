# ADR-0037: Fog and Heatmap tiles are 64 px, stretched smoothly by the clients

## Status

Accepted. Built on the server (`IMPLEMENTATION.md` §4.2); the web and Android clients are unchanged. It replaces the tile size ADR-0003 named, 512 px; the rest of ADR-0003 stands. Since ADR-0038 the clients smooth the tiles only from zoom 14 in, and draw square pixels below.

## Context

Every Fog and Heatmap tile, stored mask and per-activity mask was 512×512, about 4.8 m per pixel at z14. After the 2026-10-09 load test, the cost of a render and of an account's storage was mostly in those pixels: drawing an activity's masks, compositing and pyramiding tiles, and R2 storage, where masks alone were about 70% of an account's bytes (`IMPLEMENTATION.md` §4.2.6). Storage is the cost that grows forever per user (`VISION.md` §4.3), and long tracks were CPU-bound to draw.

The clients already draw these layers at a declared tile size of 512 and stretch whatever image the server sends with linear filtering, so a smaller image looks softer rather than blocky. The question was how small it can get before it stops reading well. `docs/PERFORMANCE.md`'s 2026-10-09 resolution session compared 512, 256, 128 and 64 px, smooth and pixel-art, on the demo account at zoom 13 and 16, and measured their storage; a true 64 px render on the dev stack was then compared with 512 px before deciding.

## Decision

**Fog and Heatmap tiles, their pyramid and every per-activity mask are 64×64** (`fog.TileSize`), about 38 m per pixel at z14, and the clients keep drawing them at 512 with smooth stretching. With it:

- The stroke keeps its real-world width: 1.25 px either side, still about 48 m.
- No box blur. At this size the stroke's anti-aliased edge, stretched by the client, is the soft edge the blur used to make.
- No dilation in the pyramid. A pixel is already about 8 screen pixels wide, so the 2×2 max pooling alone keeps a far-away track plainly visible; dilating as well would make zoomed-out tracks three times wider than before.
- Drawing a track skips points within half a pixel of the last one drawn: at this size GPS jitter makes segments short enough that `gg`'s stroker drew nothing for a whole track.

## Alternatives considered

- **Keep 512 px.** The best close-up detail, at about ten times the storage and 64 times the pixels per tile.
- **256 px.** Looked practically the same as 512 at both zooms, at 43–44% of the storage. A safe step, but it left most of the saving on the table.
- **128 px.** Softer up close, 17–20% of the storage.
- **Pixel-art at 128 or 64 px**, stretched with nearest-neighbour. Crisp and deliberately blocky: a look, not a saving over the smooth version, and jagged on diagonals.

## Consequences

- An account's Fog and Heatmap tiles take about 8–9% of their 512 px bytes as PNG, and its masks about a quarter; drawing, compositing and pyramiding a tile cost microseconds instead of milliseconds (`docs/PERFORMANCE.md`, 2026-10-09).
- Up close (zoom 16 and beyond) the edge of a cleared band is soft and gently stepped rather than crisp; a thin track far out is a soft blob a few screen pixels wide.
- The high-resolution map export (`SPEC.md` FR-4.10) captures these layers at that softness.
- Changing the tile size means redrawing every stored mask and tile: deploying it runs `rerender-coverage --masks` once (`DEPLOY.md` §6).