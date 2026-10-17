# HoldMyTrack web client

React + MapLibre GL JS on a self-hosted Protomaps basemap, described in `docs/ARCHITECTURE.md` (§1.2) and `IMPLEMENTATION.md` (§5.4).

## Getting started

Everything runs in Docker from the repository root (root `README.md`):

```bash
docker compose up                            # http://localhost:5173
docker compose run --rm web npm run basemap  # cut/re-cut the .pmtiles extract
```

The basemap archive, a 326 MB build artifact, is not tracked in git. `npm run basemap` needs the `pmtiles` CLI (`go-pmtiles` v1.31.2) on `PATH`; the Docker image carries it, so the `docker compose run` form above needs no host install. Set `DRY_RUN=1` to see the tile count and archive size without downloading anything.

Working directly in this directory also works:

```bash
npm install
npm run basemap     # needs the pmtiles CLI on PATH
npm run dev
```

## Scripts

| Script | What it does |
| :-- | :-- |
| `npm run dev` | Vite dev server; serves the archive from `public/` over range requests |
| `npm run build` | Typecheck, then build to `dist/` (~346 MB, the archive copied in) |
| `npm run verify:map` | Headless Playwright checks against the dev server (`tests/smoke.mjs`) |
| `npm run verify:build` | The same checks against the production bundle |
| `npm run basemap` | Re-cut the `.pmtiles` extract |
| `npm run typecheck` | `tsc --noEmit` |
| `npm run test:unit` | Plain `node --test` unit tests |
| `npm run test:coverage` | `test:unit` under c8, counting every file in `src/`; the report lands in `coverage/` |
| `npm run build:style` | Regenerate the style documents the API serves (`services/server/internal/mapstyle/styles`) from `src/map/style.ts` |
| `npm run verify:style` | Fail if those committed style documents have drifted from `style.ts` |

Run `npm run build` before `npm run verify:build`.

## Layout

```
scripts/build-basemap.sh   # the extract, reproducible; the region lives here
public/basemap/            # .pmtiles gitignored; fonts + sprites committed
src/map/
  config.ts                # asset paths and the default view
  protocol.ts               # registers pmtiles:// exactly once
  worker.ts                # gives MapLibre a worker URL that survives bundling
  style.ts                 # buildStyle() — pure, DOM-free, reused by headless render
  layers.ts                # layer-ordering helpers (fog/track insertion points)
  useMapInstance.ts        # map lifecycle, StrictMode-safe
  viewState.ts             # <-> URL hash
  coverageStyle.ts         # Fog and Heatmap, served in the style's metadata
  coverageLayers.ts tracks.ts mapMode.ts  # the overlays on the map and the mode switch
  exportMap.ts             # high-resolution export, rendered client-side
  MapView.tsx
src/ui/                    # ImportPanel, ActivitiesPanel and the rest of the UI
```

## Three things that are easy to break

**`style.ts` must stay pure and DOM-free.** No React, no `window`: `npm run build:style` renders it in Node for the API to serve (`docs/ARCHITECTURE.md` §2.1). That is why `buildStyle` takes an `origin` argument instead of reading `window.location`, since MapLibre 6 rejects relative sprite URLs.

**Fog and track layers go *beneath* the first symbol layer.** Fog painted over labels buries every place name and reads as a rendering bug. `layers.ts` computes that insertion point, and every overlay inserts there.

**Dev passing says nothing about the bundle.** MapLibre derives its worker URL from its own `import.meta.url`, which bundling invalidates, leaving a blank grey map with no console error and no failed request. `src/map/worker.ts` pins the URL explicitly and `npm run verify:build` guards it.