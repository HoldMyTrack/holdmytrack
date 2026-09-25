# HoldMyTrack: Development

The practical guide to running HoldMyTrack locally, verifying a change, and not rediscovering the same bug twice. `docs/ARCHITECTURE.md` covers *why* the system is shaped this way; `docs/IMPLEMENTATION.md` covers *how* each feature works; this document covers neither — it's the day-to-day operating manual.

## Running it locally

Everything runs in Docker, including the Vite dev server.

```bash
docker compose up            # http://localhost:5173
```

`make help` lists the wrapper targets. See "Verification" below for the full checklist. Working directly in `apps/web` with `npm run dev` still works and is sometimes faster; the container is the default path, not the only one.

### The backend's compose services

`db` (`postgis/postgis:16-3.4`), `rustfs` (RustFS, an S3-compatible server standing in for Cloudflare R2 locally; it replaced MinIO once MinIO stopped publishing free images), `migrate` (applies the schema, then exits), `api` (`cmd/holdmytrack serve`) and `worker` (`cmd/holdmytrack work`) all start by default with `docker compose up` — `api` and `worker` build from the *same* `services/server` image with different `command:` arguments, not two separate images, per `docs/ARCHITECTURE.md` §1.2's "one binary, two modes." `api`/`worker`/`migrate` depend on `db` being healthy; `api`/`worker` also depend on `migrate` completing successfully, so a fresh `docker compose up` can't race the schema. There's no healthcheck on `minio` specifically — `cmd/holdmytrack` retries its own Postgres and MinIO connections with backoff at startup instead of requiring one, since not every `minio` image build can be assumed to ship a specific health-check client binary.

Env vars (`POSTGRES_*`, `S3_*`) are Compose interpolation, set in `/.env` (see `.env.example`) — never in `apps/web/.env`, and never seen by Vite. No custom `networks:` block: Compose's default network already resolves `db`/`rustfs` by service name.

### Server-rendered pages in dev

Every page other than the map (About, Help, Contacts, and sign-in with the rest of the auth pages so far) is rendered by the Go server (`IMPLEMENTATION.md` §4.19), but you still open everything at `localhost:5173`: Vite's dev server proxies the page paths to `api` (`vite.config.ts`'s `pageRoutes`, target `API_PROXY_TARGET`, which compose sets to `http://api:8080`). That keeps pages and the map on one origin, as in production, which the pages' same-origin check on form POSTs relies on. `compose.yaml` bind-mounts `services/server/internal/web` into `api` with `WEB_DEV_DIR` set, so templates and `pages.css` are re-read on every request — edit one and reload, no rebuild. A change to the Go handlers themselves (`internal/httpapi/pages.go`) still needs `docker compose up -d --build api`. Running Vite on the host (`npm run dev`) proxies to `localhost:8080` unless `API_PROXY_TARGET` says otherwise. A new page path goes in three places: `registerPages` (`server.go`), `pageRoutes` (whose patterns must allow a query string — Vite matches them against the full URL), and the Caddyfile's `@backend` matcher. To try the email-verification flow, set `SKIP_EMAIL_VERIFICATION=false`: with no `SMTP_HOST`, the server logs each email, link included, instead of sending it.

### Seeding a fresh database

`migrate` creates the schema and the Demo Customer's `users` row, but no data — two one-off subcommands fill that in, and `docker compose up` runs neither. Without them, "Try it now" signs into a demo account with zero activities, and Fog/Heatmap's Country/Region zoom tiers have no boundaries to draw. Run both once against any new or recreated database — and run `seed-admin-boundaries` once against an existing database that predates the Country/Region tiers, since `migrate` creating their tables doesn't fill them and the tiers then just render blank rather than erroring:

```bash
docker compose run --rm api seed-admin-boundaries   # Natural Earth country/region polygons (IMPLEMENTATION.md §4.2.4)
docker compose run --rm api seed-demo-customer      # the Demo Customer's 611 GPX activities (IMPLEMENTATION.md §4.10 / SPEC.md FR-2.2)
```

Boundaries first, so the demo's activities get their country/region matches at ingest — the other order also works, since `seed-admin-boundaries` backfills matches for activities already present, just with one more pass. Both are idempotent: re-running skips whatever is already loaded. `seed-demo-customer` pushes every file through the real ingest pipeline, so it takes a few minutes.

### The local basemap covers Ohio only — or point dev at production's planet tiles

`apps/web/public/basemap/basemap.pmtiles` is a regional extract (roughly Ohio, z0–14, cut by `npm run basemap`), so anything outside it renders as bare background. To see the whole planet locally, add `VITE_BASEMAP_ORIGIN=https://tiles.holdmytrack.com/<YYYYMMDD>` (the dated prefix production uses, `docs/DEPLOY.md` §5) to `apps/web/.env.local` and restart `web`. That only works because the basemap bucket's CORS policy lists `http://localhost:5173` alongside the production origin — without it every tile, font and sprite request fails CORS. Each tile loaded counts as a production R2 request. `.env.local` is in `apps/web/.dockerignore`, so no image bakes it in, but the `test` service bind-mounts `apps/web` and Vite would read it from there; `compose.yaml` pins `VITE_BASEMAP_ORIGIN` (empty, same-origin) and `VITE_API_BASE_URL` on that service, and real env vars outrank every `.env` file, so `verify:map` keeps using the local extract and the local API.

## Verification

Confirmed working end to end as of 2026-09-13 (`linux/aarch64` VM, Docker 29.7.2, Compose v5.5.1) — re-confirmed repeatedly since via `verify:map`/`verify:build` after each feature, most recently password recovery. Re-run the full checklist below after any change to `compose.yaml`, either Dockerfile, or the Vite config — containerisation is the part most likely to break silently.

1. `docker compose up` → `localhost:5173` renders Columbus with streets, labels and sprites, in both light and dark.
2. Edit `apps/web/src/ui/VersionBanner.tsx` on the host → the container's Vite logs an `hmr update` line within a second or two, with no manual refresh needed. If not, set `VITE_WATCH_POLL=1` and confirm that was the cause.
3. `curl -H "Range: bytes=0-1023" http://localhost:5173/basemap/basemap.pmtiles` → `206`, never a full-file `200`.
4. `docker compose --profile test run --rm test npm run verify:map` → all 7 checks pass.
5. `docker compose --profile test run --rm test sh -c 'npm run build && npm run verify:build'` → all 4 checks pass, including the worker-asset check.
6. `docker compose run --rm web pmtiles show public/basemap/basemap.pmtiles` → z0–14, 63,948 addressed tiles, proving no host binary is needed.
7. `cd apps/web && npm run dev` still works and binds loopback-only (`Network: use --host to expose`) since `VITE_DEV_HOST` is unset outside the container — the container is the default path, not the only one.

Step 4 was the riskiest part of the whole design going in: SwiftShader WebGL under headless Chromium on `linux/arm64` was the one combination that had never been verified. It passed cleanly on first run.

### Backend verification

Also confirmed against a live stack, not assumed:

- `docker compose up` brings up `web`, `db`, `rustfs`, `migrate`, `api` and `worker` together; `migrate` applies every `*.sql` file under `services/server/migrations/` (embedded via `go:embed`, tracked in `schema_migrations`) and exits.
- A real `.gpx` upload (`curl -F file=@sample.gpx http://localhost:8080/v1/activities/upload`) round-trips through the job queue into `activities` and `activity_streams`, with a correctly typed `LINESTRING M` trajectory.
- A real `.tcx` upload round-trips the same way, including heart rate.
- **Idempotency** (`IMPLEMENTATION.md` §4.0's explicit invariant): the same file uploaded twice sequentially returns `already_processed` from the fast-path check; five *concurrent* uploads of identical new content all enqueue (the fast path can't see each other), but the persist-time `ON CONFLICT (user_id, source, external_id) DO NOTHING` still collapses them to exactly one `activities` row and one `activity_streams` row.
- Malformed input is rejected before processing: unsupported extensions (415) and empty files (400) never reach the parser or the queue.
- **Track tiles** (`IMPLEMENTATION.md` §4.3): `GET /tiles/v1/tracks/{z}/{x}/{y}.mvt` returns valid, non-empty MVT bytes with a `tracks` layer for a tile covering an uploaded activity, and a clean `200`/0-byte response (not an error) for a tile with none. End to end through the real UI, not just curl: uploading a file through `ImportPanel`'s Files tab renders it on the map — confirmed via `querySourceFeatures`/`queryRenderedFeatures` in a real Playwright-driven browser session, not assumed from the component wiring — with zero manual page refresh.

### CI

`.github/workflows/ci.yml` runs on every push to `main` and every pull request, in three parallel jobs: `go test ./...`; the web client's `typecheck`, `test:unit` and `verify:style` (on the runner's own Node, not in Docker); and `make verify verify-build` against the full compose stack. The basemap extract isn't in git, so that last job caches it keyed on `apps/web/scripts/build-basemap.sh` and cuts it with `make basemap` on a miss — which fails with a 404 once the script's pinned Protomaps build has aged out of their bucket (about a week); bump `BUILD` in the script. Screenshots and the `api`/`migrate` logs are kept when that job fails. The Go job also uploads its coverage profile to Codecov (the README badge, and a comment on each PR). That's the Go unit tests only, since the Playwright suites aren't instrumented, so the number is low by design; `codecov.yml` keeps both of Codecov's status checks informational so coverage never blocks a merge. `make test` runs the same set locally. CI never deploys: auto-deploy waits on backups existing (`docs/ROADMAP.md`, "Production deployment").

This verification pass is what caught two of the gotchas below (the PostGIS M-ordinate bug and the `styledata`/`isStyleLoaded` timing bug) — see "Gotchas worth not rediscovering."

## Gotchas worth not rediscovering

All verified directly against the repo, the registries, or a real `docker compose` run — not guessed.

- **Never bind-mount host `node_modules` into a container.** Vite 8 is Rolldown-based and the tree contains native Mach-O binaries (`@rolldown/binding-darwin-arm64`, `lightningcss-darwin-arm64`). A Linux container handed macOS binaries dies on startup. `compose.yaml` puts a container-owned named volume at `/app/node_modules` for exactly this reason, and `apps/web/docker/entrypoint.sh` reconciles that volume against the lockfile hash when it goes stale.
- **Headless Chromium needs an explicit SwiftShader flag or MapLibre never renders.** There is no GPU in the container, and since roughly Chrome 130 Chromium refuses to fall back to software rasterisation for WebGL silently. Without `--enable-unsafe-swiftshader` (plus `--use-gl=angle --use-angle=swiftshader`) the map never acquires a context and the test failure does not name its cause. The flags are passed as `PW_CHROMIUM_ARGS` by the `test` compose service and read by `tests/smoke.mjs` and `tests/build.mjs`'s `chromium.launch()` calls — confirmed working end to end (this document, "Verification").
- **The `go-pmtiles` image has no shell and `WorkingDir "/"`.** The binary is at **`/go-pmtiles`**, not `/pmtiles`. Two consequences: `docker run ... protomaps/go-pmtiles <args>` needs `-w /work` or relative output paths are written inside the container and lost, and `scripts/build-basemap.sh` cannot run inside that image at all. The Dockerfile copies the binary into the dev image at `/usr/local/bin/pmtiles` instead.
- **`vite.config.ts` needs its own `process.env` ambient type.** `tsconfig.json`'s `types` is deliberately just `["vite/client"]` — no `@types/node` for the sake of one config file reading `VITE_DEV_HOST`/`VITE_WATCH_POLL`. The file declares `process` locally instead. `npm run build` runs `tsc --noEmit` first, so this breaks loudly if a future edit reintroduces a bare `process` reference elsewhere.
- **Playwright is pinned exactly, not caret-ranged.** It had drifted (`^1.56.0` resolved to 1.63.0) because Playwright ships browser builds keyed to its own version; `package.json` now pins `1.63.0` directly so the npm dependency and the image's browser build can't diverge again.
- **`ST_SimplifyPreserveTopology` drops the M ordinate in PostGIS 3.4.** Confirmed directly against this stack, not assumed: simplifying a `LineStringM` returns a plain `LINESTRING`, no M at all (`SELECT GeometryType(...)` on the simplified result confirms it) — `IMPLEMENTATION.md`'s "M dimension carries epoch seconds" requirement silently breaks if you call it on the timestamped trajectory directly. `internal/ingest/simplify.go` simplifies X/Y only, then reattaches each surviving vertex's original timestamp by matching coordinates in original sequence order — valid because Douglas-Peucker-style simplification only selects a subset of the original vertices, never moves or interpolates them. If you touch trajectory simplification, read that file's doc comment before "simplifying" it back to one call. First caught by backend verification: a real `.gpx` upload round-tripped with a trajectory that had silently lost its M ordinate.
- **A `styledata`-only listener can silently never fire on first load, and adding a source afterward makes `isStyleLoaded()` dip back to `false`.** Two related gotchas found together adding the tracks layer. First: `MapView`'s overlay-attach function was wired only to the `styledata` event, but that listener itself gets attached *after* `map` state is set, which is itself set inside the initial `'load'` handler — a later `styledata` isn't guaranteed to fire again on its own, and on first page load it didn't, so the tracks overlay silently never attached until a theme toggle triggered a real restyle. Fixed by calling the overlay-attach function once directly, not only from the listener. Second: that same overlay attach adds a new source, which makes `isStyleLoaded()` dip back to `false` for a moment while its first tiles load — `tests/smoke.mjs`'s `styleLoaded()` helper now waits for the flag to hold `true` across two checks 150ms apart, not just the first `true` it sees. If you add fog the same way, this is already handled for you; if you add any *other* async-loading thing to the map, remember `isStyleLoaded()` isn't a one-shot signal once overlays exist.
- **Inside the `test` compose service, `localhost` is that container, not `api`.** Chromium in `tests/smoke.mjs`/`tests/build.mjs` runs *inside* the `test` container, so `apps/web/src/api.ts`'s default `API_BASE_URL` (`http://localhost:8080`, correct for the `web` service where the browser runs on the host) resolves to nothing there. The fix is `network_mode: "service:api"` on `compose.yaml`'s `test` service, which puts the test container on `api`'s network stack so `localhost:8080` reaches it — and keeps the browser, the test's Vite server and `api` same-site, so the `SameSite=Lax` session cookie is still attached (a service-name `api:8080` base lost it). Discovered because the tracks source made this concrete: an unreachable-inside-the-test-network tile source, not a flaky one.
- **The idempotency guarantee lives at persist time, not receive time.** `internal/httpapi`'s upload handler checks for an existing `(user_id, source, external_id)` before enqueuing, but that's a fast path only — two identical uploads can both pass it while racing. The actual guarantee is `internal/ingest.Process`'s `INSERT ... ON CONFLICT (user_id, source, external_id) DO NOTHING`, verified against a real race: 5 concurrent uploads of identical content all completed their jobs, and exactly one row landed in `activities`. If you add a second job kind that writes `activities`, it needs the same `ON CONFLICT`, not just a check-then-insert.
- **`map.setFilter`/`map.setLayoutProperty` (the public `Map` methods) call `_update(true)` unconditionally, even when the value is unchanged** — confirmed by reading the installed maplibre-gl 6.9.0 bundle directly, not assumed. That marks sources dirty and forces a full source re-check on the next render frame regardless of whether anything actually needs to change. `reattachOverlays` (`MapView.tsx`) re-applies `setMapMode` and `setHiddenTracks` on every `'styledata'` event by design (`addLayer` always resets visibility/filter, so a `styledata`-triggered layer recreation would otherwise silently undo the current mode/hidden-set). Calling either unconditionally from inside that handler self-sustains: `setFilter`/`setLayoutProperty` → `_update(true)` → next frame re-fires `'styledata'` → handler runs again → calls them again → repeat forever, and `isStyleLoaded()` never settles. Root-caused this exact way after `setHiddenTracks` (added for the eye icon) started making it fatal — the live, network-backed tracks source made the forced per-frame re-check expensive enough that `verify:map`'s test timed out instead of merely wasting cycles. Both `mapMode.ts`'s `setVisible` and `tracks.ts`'s `setHiddenTracks` now diff against the layer's current value first and skip the call when nothing would change; if you add a third thing to `reattachOverlays` that calls a `map.set*` method, give it the same guard.
- **`go:embed` can't reach across `services/server/internal/db/` into `services/server/migrations/`.** Patterns can't cross out of the directory containing the source file, and the SQL has to live in `migrations/` (services/server/README.md commits to that). `migrations/embed.go` is the one file that lives beside the `.sql` and embeds it; `internal/db` imports that package rather than embedding directly.

## Commands

Local development runs in Docker; the containerised path is the default, not the only one.

```bash
docker compose up                   # web on :5173, api on :8080, plus db/rustfs/worker
docker compose --profile test run --rm test npm run verify:map
docker compose --profile test run --rm test sh -c 'npm run build && npm run verify:build'
docker compose run --rm web pmtiles show public/basemap/basemap.pmtiles

curl -F file=@activity.gpx http://localhost:8080/v1/activities/upload
docker compose exec db psql -U holdmytrack -d holdmytrack -c 'SELECT * FROM activities;'
docker compose logs worker -f               # watch ingest jobs process
```

The `Makefile` at the root wraps these; `make help` lists the targets. `make test` runs every suite CI runs: Go unit tests (`make test-go`, in a throwaway `golang` container), the web unit tests and style-drift check (`make test-unit`), then `verify` and `verify-build`.

Working inside the web client directly (`cd apps/web`) also works and is sometimes faster:

| Script | What it does |
| :-- | :-- |
| `npm run dev` | Vite dev server on 5173, serving the archive over HTTP range requests |
| `npm run build` | `tsc --noEmit`, then `vite build` to `dist/` |
| `npm run typecheck` | `tsc --noEmit` alone |
| `npm run basemap` | Re-cut the `.pmtiles` extract (needs the `pmtiles` CLI on `PATH`) |
| `npm run verify:map` | Headless Playwright checks against a dev server it spawns itself |
| `npm run verify:build` | The same questions asked of the production bundle, which is not the same question |
| `npm run test:unit` | Plain `node --test` unit tests (the track editor's edit ops) |
| `npm run verify:style` | Fails if the style documents under `services/server/internal/mapstyle/styles` have drifted from `src/map/style.ts`; `npm run build:style` regenerates them |

Run `npm run build` before `npm run verify:build`.

## Known costs

"Everything in Docker" is a real trade, not a free win:

- **The IDE still needs a host `node_modules`.** Roughly 300 MB of darwin-only packages the container never touches, kept only so tsserver works, refreshed by hand when the lockfile changes.
- **Adding a dependency is a three-step operation** — install in the container, rebuild the image, then install on the host for the IDE. Previously one command.
- **`vite build` copies the 326 MB archive through VirtioFS** instead of letting APFS `clonefile` it. Near-instant becomes a few seconds.
- **Playwright renders on SwiftShader, not Metal.** The verification suites assert against a rasteriser no user will ever have — a genuine fidelity loss in exactly the tests written to catch rendering bugs.
- **HMR gains a failure mode that doesn't exist on the host.** When it goes deaf the question is about VirtioFS, not the code.

One open follow-up, cheap now and irritating later: `scripts/build-basemap.sh` pins a dated basemap build key that Protomaps' rolling seven-day retention window ages out silently. Teaching the script to resolve the newest key from `https://build-metadata.protomaps.dev/builds.json` when `BUILD` is unset (keeping the pinned value as an explicit override) would turn a future 404-with-no-obvious-cause into a normal fresh-clone experience — and into a self-healing cache miss in CI, whose map job cuts the extract with this same script whenever its cache is empty ("CI" above).