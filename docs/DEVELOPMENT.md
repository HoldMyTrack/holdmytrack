# HoldMyTrack: Development

Running HoldMyTrack locally, verifying a change, and bugs already hit once.

## Running it locally

Everything runs in Docker, the Vite dev server included: `docker compose up`, then open `http://localhost:5173`. `make help` lists the wrapper targets.

### The backend's compose services

`docker compose up` starts `web`, `db` (PostGIS), `rustfs` (S3-compatible, standing in for Cloudflare R2), `migrate` (applies the schema, then exits), `api` (`holdmytrack serve`) and `worker` (`holdmytrack work`), the last two from one image (`docs/ARCHITECTURE.md` §1.2). Both wait for a healthy `db` and a successful `migrate`. `rustfs` has no healthcheck; `holdmytrack` retries its Postgres and S3 connections at startup instead. Env vars (`POSTGRES_*`, `S3_*`) are Compose interpolation from the root `.env` (see `.env.example`), never `apps/web/.env`.

### Server-rendered pages in dev

Every page, the map's shell included, is rendered by Go (`IMPLEMENTATION.md` §4.19), but you open everything at `localhost:5173`. Vite proxies the page paths (`pageRoutes` in `vite.config.ts`) to `API_PROXY_TARGET` (`http://api:8080` in compose, `localhost:8080` by default on the host) and marks each request `X-HoldMyTrack-Vite: dev`; a dev Go server then answers with a shell that loads the app from Vite, with Fast Refresh. `vite preview` proxies without the marker, so the shell loads the build. Both `verify:map` and `verify:build` need a running Go server.

`compose.yaml` mounts `services/server/internal/web` into `api` with `WEB_DEV_DIR` set, so templates and stylesheets are re-read on every request; a Go handler change needs `docker compose up -d --build api`. A new page path goes in both `registerPages` (`server.go`) and `pageRoutes`, whose patterns must allow a query string; a path missing from either 404s in dev only, since production's Caddy sends every non-static path to Go.

To try email verification, set `SKIP_EMAIL_VERIFICATION=false`: with no `SMTP_HOST`, the server logs each email, link included.

The Android suite's sample files (`/testing`) are generated. After changing `demo_data/` or `cmd/make-testing-files`, run this from `services/server` and commit what it writes to `internal/web/static/testing/`:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25-bookworm go run ./cmd/make-testing-files
```

Use the Go version `go.mod` names: another release compresses the zips and encodes the JPEGs differently, and the Go tests fail while the committed files differ from the output.

### Text and translations

Every piece of text a person reads is a catalog key in English and Russian (`IMPLEMENTATION.md` §4.21), and new text goes into both in the same change:

- **Server page, email or error:** `services/server/internal/i18n/locales/en.json` and `ru.json`; `{{t "key"}}` in a template (`tn` for a count, `th` for markup), `l.T("key")`, `accountFailure(status, "key")` or `httpErrorT(w, r, status, "key")` in Go. About and Help are whole templates: edit `about.html`/`help.html` and their `.ru.html` twins together.
- **Map app:** `apps/web/src/i18n/en.ts` and `ru.ts`, as `t('key')` or `tn('key', n)`; `tsc` fails until `ru.ts` has the key.
- **Android:** `res/values/strings.xml` and `res/values-ru/strings.xml`; Lint's `MissingTranslation` catches a gap.

A count needs every plural form: `.one`/`.other` in English, `.one`/`.few`/`.many`/`.other` in Russian. Placeholders stay identical. `go test ./internal/i18n` and `npm run test:unit` check both. To see Russian, use the language menu or `curl -H 'Accept-Language: ru' localhost:5173/help`; the menu's `hmt_lang` cookie beats the header and an account's Language setting beats both.

Adding a language takes a catalog on each surface, `about`/`help` translations, its plural rule (`i18n.PluralForm`) and separators (`Localizer.separators`), its code in `i18n.Supported`/`Names`, `locales_config.xml` and `build.gradle.kts`'s `localeFilters`, then `npm run build:style`.

### Seeding a fresh database

`migrate` creates the schema and the Demo Customer's `users` row but no data, and `docker compose up` runs no seeds. Without them "Try it now" opens an empty account, Fog's Country/Region tiers have no boundaries, and activities show their account's timezone. Make the boundaries file with `scripts/boundaries-extract.sh ~/boundaries` (`docs/DEPLOY.md` §6; needs the [duckdb](https://duckdb.org/docs/installation) CLI), then:

```bash
curl -sSfL -o ~/boundaries/timezones-with-oceans-2026d.geojson.zip https://github.com/evansiroky/timezone-boundary-builder/releases/download/2026d/timezones-with-oceans.geojson.zip
docker compose run --rm -v ~/boundaries:/data:ro api seed-admin-boundaries --file /data/overture-2026-09-23.0-boundaries.csv.gz   # IMPLEMENTATION.md §4.2.4
docker compose run --rm -v ~/boundaries:/data:ro api seed-timezones --file /data/timezones-with-oceans-2026d.geojson.zip       # IMPLEMENTATION.md §4.30
docker compose run --rm api seed-demo-customer   # IMPLEMENTATION.md §4.10, SPEC.md FR-2.2
```

Each takes a few minutes. Seeding boundaries and timezones first matches the demo at ingest; either order works. All three skip what is already loaded (`--force` makes either seed reload). `seed-demo-customer` never changes an activity already seeded, so after `demo_data/` changes run it once with `--reset`, which deletes the demo's activities first.

After a change to how Fog/Heatmap tiles are drawn (`internal/fog`), `docker compose run --rm api rerender-coverage` queues a re-render of every account's tiles (`--user <id>` for one; `--masks` to redraw masks first, after a stroke change).

Spots (`SPEC.md` FR-15) needs an import: cut a small extract with `scripts/spots-extract.sh <work-dir> <geofabrik-pbf-url>` from a [Geofabrik](https://download.geofabrik.de/) region (`docs/DEPLOY.md` §6), put `spots.geojsonseq` in `/tmp/spots`, and run `docker compose run --rm -v /tmp/spots:/data:ro api import-spots /data/spots.geojsonseq`. Bike paths (`SPEC.md` FR-4.13) likewise: `scripts/bike-paths-extract.sh <work-dir> <geofabrik-url>` (the Spots work-dir reuses its download), then `docker compose run --rm -v /tmp/bike-paths:/data:ro api import-bike-paths /data/bike-paths.geojsonseq`.

### Admin, stats and the Android WebView

For the admin panel (`/admin`, `IMPLEMENTATION.md` §4.20), sign up locally and run `docker compose run --rm api set-admin you@example.com true`.

`worker` takes the daily statistics (`IMPLEMENTATION.md` §4.32) within an hour of midnight UTC and at its start. `docker compose run --rm api stats` prints the newest day, `stats 2026-10-13` one day, `stats activities_by_source --days 90` a key's (or prefix's) series. `stats --take [day]` writes a snapshot now, yesterday by default, but with today's state values, so use it only locally. To retake a day, delete its `daily_stats` rows.

The Android app shows Settings, Profile, Sync and About as these pages in a WebView. Against the stock dev stack their forms get `403`: a form must come from `APP_BASE_URL`'s origin (`localhost:5173`), while the emulator reaches `10.0.2.2:8080`. To try them, run `adb reverse tcp:8080 tcp:8080` and set `APP_BASE_URL=http://localhost:8080` for `api` and `holdmytrack.apiBaseUrl=http://localhost:8080` for the app.

### Dev uses the planet basemap from `tiles.holdmytrack.com`

Set `VITE_BASEMAP_ORIGIN=https://tiles.holdmytrack.com/<YYYYMMDD>` (production's dated prefix, `docs/DEPLOY.md` §5) in `apps/web/.env.local`, which is gitignored, and restart `web`. This works because the zone answers basemap responses with `Access-Control-Allow-Origin: *` and the bucket's CORS policy lists `http://localhost:5173` for preflights; the zone refuses every other `Origin`, so a dev server on another port gets `403`. Each tile counts as a production R2 request.

The Playwright suites use a regional extract, `apps/web/public/basemap/basemap.pmtiles` (Ohio, z0–14, cut by `make basemap`). The `test` service bind-mounts `apps/web`, so Vite would read `.env.local` there; `compose.yaml` pins `VITE_BASEMAP_ORIGIN` (empty) and `VITE_API_BASE_URL` on that service, and real env vars outrank `.env` files.

### Satellite imagery in dev

Off unless configured (`docs/SPEC.md` FR-4.14). For the web, add `VITE_SATELLITE_TILES`, `VITE_SATELLITE_TILE_SIZE`, `VITE_SATELLITE_MAXZOOM` and `VITE_SATELLITE_ATTRIBUTION` to `apps/web/.env.local` and restart `web`; for Android's style document, set `SATELLITE_*` in the root `.env` and recreate `api`. A MapTiler key needs `http://localhost:5173` in its allowed origins. For a keyless local check (never a deployment), use Esri World Imagery: `https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}`, tile size 256. `tests/smoke.mjs` step 9 tests the switch when `VITE_SATELLITE_TILES` is set and its absence otherwise.

## Verification

Re-run this after any change to `compose.yaml`, a Dockerfile or the Vite config:

1. `docker compose up` → `localhost:5173` renders Columbus with streets, labels and sprites, in light and dark.
2. Edit `apps/web/src/ui/VersionBanner.tsx` on the host → the container's Vite logs `hmr update` within a couple of seconds. If not, try `VITE_WATCH_POLL=1`.
3. `curl -H "Range: bytes=0-1023" http://localhost:5173/basemap/basemap.pmtiles` → `206`, never `200`.
4. `make verify` → all 7 checks pass.
5. `make verify-build` → all 4 checks pass.
6. `docker compose run --rm web pmtiles show public/basemap/basemap.pmtiles` → z0–14, 63,948 addressed tiles.
7. `npm run dev` in `apps/web` still works and binds loopback-only, since `VITE_DEV_HOST` is unset outside the container.

### Backend verification

After a change to ingest, the queue or tiles, check against a live stack:

- `curl -F file=@sample.gpx http://localhost:8080/v1/activities/upload` lands in `activities` with a `LINESTRING M` trajectory (`docker compose logs worker -f` shows the job); a `.tcx` likewise, heart rate ignored (ADR-0017). An unsupported extension gets 415 and an empty file 400.
- The same file twice returns `already_processed`; five concurrent uploads of new identical content leave one row (`IMPLEMENTATION.md` §4.0).
- `GET /tiles/v1/tracks/{z}/{x}/{y}.mvt` returns a `tracks` layer over an upload and an empty `200` elsewhere; a file uploaded through the Upload menu appears on the map without a refresh.

### Go database tests

Go tests that need PostGIS use the database `TEST_DATABASE_URL` names (`internal/httpapi/dbtest_test.go`) and skip without it. They migrate it (under an advisory lock) and delete the accounts they create. `make test-go` creates `holdmytrack_test` in compose's `db` and runs the suite. With the stack up, the same from `services/server`, after a one-time `docker compose exec db createdb -U holdmytrack holdmytrack_test`:

```bash
TEST_DATABASE_URL=postgres://holdmytrack:holdmytrack@localhost:5432/holdmytrack_test go test -p 1 ./...
```

Several packages share that database, so the suite runs one package at a time (`-p 1`, in `make test-go` and CI): the worker's tests claim any runnable job, another package's included. A test that reads back stored objects uses `internal/storage/storagetest`'s in-memory S3.

Every request `internal/httpapi`'s tests send through `dbTest.do`, `dbTest.send` or `serveChecked` is checked against the API contract in `services/server/api/` (`IMPLEMENTATION.md` §4.31). A failure names the exchange and schema path; fix whichever side is wrong. A new endpoint fails `TestContractCoversEveryRoute` until its operations are in `api/paths/` and its path in `openapi.yaml`'s index; mark response objects `additionalProperties: false`. `go test -run TestContract ./internal/httpapi` runs without a database. After a contract change, run `npm run gen:api-types` in `apps/web` (CI's `verify:api-types` fails while `src/apiSchema.ts` is stale) and the Android tests, whose `WireContractTest` checks `net/Wire.kt`.

### Android verification

The app has JUnit tests over its pure logic (`./gradlew :app:testDebugUnitTest` in `apps/android/holdmytrack`) and no instrumentation tests or Health Connect test double; the rest is checked by hand. After a change to the sync code, re-check on a physical device with real Health Connect data: a run cut off midway, a route not handed over, a reinstall (synced sessions must not come back) and an activity deleted on the web (its session must). Only one device and one provider (Fitbit) have been tested; Samsung's missing routes rest on Samsung's documentation.

### CI

`.github/workflows/ci.yml` runs on every push to `main` and every pull request, in six parallel jobs: `scripts/check-doc-refs.sh`, which fails on a `§`, FR or ADR citation whose heading no longer exists; `scripts/check-file-size.sh`, which annotates a source file a change grows past 600 lines as a split candidate and never fails; `go test -p 1 ./...` with a PostGIS service; the web `typecheck`, `test:coverage`, `verify:style` and `verify:api-types`; the Android JUnit tests; and `make verify verify-build` against the compose stack. That last job caches the basemap extract keyed on `apps/web/scripts/build-basemap.sh` and cuts it on a miss, which 404s once the pinned Protomaps build ages out (about a week): bump `BUILD` in the script.

The Go, web and Android jobs upload unit-test coverage to Codecov, informational only (`codecov.yml`) and low by design: c8's `all` counts untested files. CI never deploys (`docs/DEPLOY.md` §6).

## Writing a migration

A deploy runs new migrations while the previous release's `api` and `worker` still serve (`DEPLOY.md` §7), so a migration must work with the old code too:

- **Fine:** a new table, a nullable or defaulted column, a new index, a short `UPDATE`, queueing jobs.
- **Not fine:** dropping or renaming what the old code uses, `NOT NULL` without a default on a column it inserts into, changing a column's type, a constraint it can break, or a lock held for more than a moment (rewriting a large table, a long backfill).

Removing or renaming takes two releases: stop using it, then drop it. A migration that can't be compatible starts with `-- Deploy: maintenance — <why>`, and the deploy runs it in maintenance mode (`DEPLOY.md` §7); without that line it runs with the site up.

## Gotchas worth not rediscovering

- **Never bind-mount host `node_modules` into a container.** Its native macOS binaries (Rolldown, lightningcss) kill a Linux container. `compose.yaml` mounts a named volume at `/app/node_modules`, which `apps/web/docker/entrypoint.sh` reconciles against the lockfile hash.
- **Headless Chromium needs SwiftShader flags or MapLibre never renders.** With no GPU, Chromium won't fall back to software WebGL on its own, and the test failure doesn't name the cause. The `test` service passes `--enable-unsafe-swiftshader --use-gl=angle --use-angle=swiftshader` as `PW_CHROMIUM_ARGS`.
- **The `go-pmtiles` image has no shell and `WorkingDir "/"`.** The binary is `/go-pmtiles`; `docker run` needs `-w /work` or relative outputs stay in the container, and `build-basemap.sh` can't run there. The dev image copies the binary to `/usr/local/bin/pmtiles`.
- **`vite.config.ts` declares its own `process`.** `tsconfig.json`'s `types` is only `["vite/client"]`, so a bare `process` elsewhere fails `tsc`.
- **Playwright is pinned exactly.** Its browser build is keyed to its version; a caret range let the package drift from the image's browser.
- **`ST_SimplifyPreserveTopology` drops the M ordinate in PostGIS 3.4**, returning a plain `LINESTRING` without the epoch seconds. `internal/ingest/simplify.go` simplifies X/Y, then reattaches each surviving vertex's timestamp by matching coordinates in order; read its doc comment before collapsing it to one call.
- **A `styledata`-only listener can miss first load, and adding a source makes `isStyleLoaded()` dip to `false`.** The listener is attached after `'load'`, so `MapView` also calls its overlay attach directly. A new source drops `isStyleLoaded()` while its tiles load, so `tests/smoke.mjs`'s `styleLoaded()` waits for `true` on two checks 150ms apart. Once overlays exist, `isStyleLoaded()` is no one-shot signal.
- **Inside the `test` service, `localhost` is that container.** `network_mode: "service:api"` puts it on `api`'s network stack so `localhost:8080` works and browser, Vite and `api` stay same-site for the `SameSite=Lax` session cookie; a base of `api:8080` loses the cookie.
- **Idempotency lives at persist time.** The upload handler's existing-row check is a fast path racing uploads can both pass; the guarantee is `internal/ingest.Process`'s `ON CONFLICT (user_id, source, external_id) DO NOTHING`. Any new job kind writing `activities` needs the same.
- **`map.setFilter`/`map.setLayoutProperty` call `_update(true)` even when nothing changes** (maplibre-gl 6.9.0). `reattachOverlays` (`MapView.tsx`) re-applies mode and hidden tracks on every `'styledata'`, since `addLayer` resets them, so an unconditional setter there loops forever (setter → `'styledata'` → setter) and `isStyleLoaded()` never settles. `mapMode.ts`'s `setVisible` and `tracks.ts`'s `setHiddenTracks` diff first; any `map.set*` call on that path needs the same guard.
- **`go:embed` can't reach from `internal/db/` into `migrations/`.** `migrations/embed.go` embeds the SQL beside it and `internal/db` imports that package.

## Commands

`make help` lists the wrapper targets; `make test` runs every CI suite except the doc check and the Android tests. Not wrapped:

```bash
scripts/check-doc-refs.sh                                         # citation check
scripts/check-file-size.sh                                        # soft size warnings
cd apps/web && npm run gen:api-types                              # after a contract change
cd apps/web && npm run build:style                                # after src/map/style.ts changes
cd apps/android/holdmytrack && ./gradlew :app:testDebugUnitTest   # Android JUnit tests
```

On the host, `npm run build` must precede `verify:build`, and `npm run basemap` needs `pmtiles` on `PATH`.

## Known costs

- **The IDE needs a host `node_modules`**, about 300 MB, refreshed by hand when the lockfile changes.
- **Adding a dependency takes three steps**: install in the container, rebuild the image, install on the host.
- **`vite build` copies the 326 MB archive through VirtioFS**, taking seconds.
- **Playwright renders on SwiftShader**, a rasteriser no user has.
- **HMR can go deaf over VirtioFS.**

Open follow-up: `scripts/build-basemap.sh` pins a Protomaps build that their seven-day retention ages out. Resolving the newest key from `https://build-metadata.protomaps.dev/builds.json` when `BUILD` is unset would make CI's cache miss self-healing ("CI" above).