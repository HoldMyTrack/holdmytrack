# FitMap web client

React + MapLibre GL JS on a self-hosted Protomaps basemap. This is the Phase 1 foundation described in `docs/ARCHITECTURE.md` (§1.2), `IMPLEMENTATION.md` (§5.4) and `VISION.md` (§5.2).

## Getting started

Everything runs in Docker from the repository root — see the root `README.md` for the full picture. The short version:

```bash
docker compose up                            # http://localhost:5173
docker compose run --rm web npm run basemap  # cut/re-cut the .pmtiles extract
```

The basemap archive is not tracked in git — it is a 326 MB build artifact. `npm run basemap` needs the `pmtiles` CLI (`go-pmtiles` v1.31.2) on `PATH`; the Docker image carries it, so the `docker compose run` form above needs no host install. Set `DRY_RUN=1` to see the tile count and archive size without downloading anything.

Working directly in this directory also works and is sometimes faster:

```bash
npm install
npm run basemap     # needs the pmtiles CLI on PATH
npm run dev
```

## Scripts

| Script | What it does |
| :-- | :-- |
| `npm run dev` | Vite dev server; serves the archive from `public/` over range requests |
| `npm run build` | Typecheck, then build to `dist/` (~346 MB — the archive is copied in) |
| `npm run verify:map` | Headless Playwright checks against the dev server (`tests/smoke.mjs`) |
| `npm run verify:build` | The same question asked of the production bundle, which is not the same question |
| `npm run basemap` | Re-cut the `.pmtiles` extract |
| `npm run typecheck` | `tsc --noEmit` |

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
  MapView.tsx
src/ui/                    # UploadPanel and the rest of the UI
```

## Three things that are easy to break

**`style.ts` must stay pure and DOM-free.** No React, no `window`. It is the seam the print pipeline needs to render an identical map headlessly at print DPI. This is why `buildStyle` takes an `origin` argument instead of reading `window.location` — MapLibre 6 rejects relative sprite URLs, and the fix had to not cost the purity.

**Fog and track layers go *beneath* the first symbol layer.** Fog painted over labels buries every place name and reads as a rendering bug. `layers.ts` computes that insertion point; nothing uses it yet, and that is deliberate.

**Dev passing tells you nothing about the bundle.** MapLibre derives its worker URL from its own `import.meta.url`, which bundling invalidates — the result is a blank grey map with no console error and no failed request. `src/map/worker.ts` pins the URL explicitly and `npm run verify:build` guards it.