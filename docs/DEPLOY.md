# Deploying HoldMyTrack — minimal single-VPS setup

The smallest deployment that's actually production-shaped: one small VPS running `compose.prod.yml` (Postgres+PostGIS, `api`, `worker`, and Caddy in front of the built frontend), plus Cloudflare R2 for object storage. See `docs/VISION.md` §4.3 for the cost model this is built around, and `docs/ROADMAP.md`'s "Production deployment" section for what's still open on the operational side (backups, host hardening, monitoring); spend caps and the compliance work (DPIA, EU-region hosting) are that file's Phases 5 and 6. This document only covers getting a working deployment live, not everything a real public launch needs.

## 1. Provision the VPS

Any small Docker-capable VPS works — 2 vCPU / 4 GB RAM is comfortable headroom for Postgres+PostGIS, `api`, `worker`, and Caddy together at low traffic. Install Docker and the Compose plugin (`docker compose version` should work), then clone this repo onto it.

## 2. Create the Cloudflare R2 bucket

1. Create an R2 bucket in the Cloudflare dashboard.
2. Create an R2 API token scoped to that bucket (Account → R2 → Manage API Tokens) — this gives you the access key, secret key, and the account-specific S3-compatible endpoint (`https://<account-id>.r2.cloudflarestorage.com`). None of this is the same as a Cloudflare account-wide API token.
3. No code change is needed for this — `internal/storage/storage.go` already talks to any S3-compatible endpoint via `minio-go`; local dev's MinIO container is a stand-in for exactly this.

## 3. Point DNS at the server

Add an `A` record for your domain (or a subdomain, e.g. `app.example.com`) pointing at the VPS's public IP. Caddy (step 6) needs this to resolve correctly *before* it first starts, or its automatic Let's Encrypt certificate request will fail. Also point `www.<your-domain>` at the same server (a `CNAME` to the apex is enough): Caddy always redirects `www` to the apex and requests a certificate for it, and so for any retired domain listed in `REDIRECT_DOMAINS`. With the zone on Cloudflare, keep these records DNS-only (grey cloud) — Cloudflare's proxy caps uploads at 100 MB, below what a bulk export archive can reach.

## 4. Fill in the production env file

```
cp .env.prod.example .env.prod
```

Fill in every value — see that file's own comments for what each one means and why it has no default (unlike dev's `.env.example`, nothing here is safe to leave as a placeholder). `APP_BASE_URL` and `DOMAIN` both need the real domain from step 3; get `APP_BASE_URL`'s `https://` scheme right — `auth.go`'s session cookie derives its `Secure` flag from it, and password-reset emails link back into it.

**Sign in with Google (optional).** Leave `GOOGLE_CLIENT_ID` empty to run without it; the sign-in screen then shows only email and password. To turn it on:

1. In the Google Cloud console, create a project (or reuse one) and set up the OAuth consent screen: user type External, app name HoldMyTrack, the domain from step 3 as an authorized domain, and only the `openid`, `email` and `profile` scopes — none of them needs Google's app verification. Publish it ("In production"); in "Testing" only listed test users can sign in.
2. Under Credentials, create an OAuth client ID of type **Web application** with the authorized redirect URI `https://<your-domain>/v1/auth/google/callback`, exactly `APP_BASE_URL` plus that path. No JavaScript origins are needed, since the flow runs server-side.
3. Put the client ID and secret into `.env.prod` as `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` and leave `GOOGLE_REDIRECT_URL` empty; it defaults to that same URI. Recreate `api` (`up -d`) to pick them up; no rebuild is needed, because the web client asks the API at runtime whether to show the button.

## 5. Basemap

The basemap is the full Protomaps planet build (z0–15, ~138 GB), served unmodified from a public R2 bucket. The archive is deliberately excluded from every Docker build context (`apps/web/.dockerignore`), so the `web` image never has it baked in.

1. Create a second R2 bucket for the basemap (e.g. `holdmytrack-basemap`), separate from the app's private one, and enable public access on it. Public access is either the bucket's `r2.dev` URL, which is rate-limited and meant for development, or a custom domain, which needs the domain's DNS zone on Cloudflare.
2. Add a CORS rule to that bucket: allowed origins `https://<your-domain>`, allowed methods `GET, HEAD`, allowed headers `range, if-match`, exposed headers `etag` (`IMPLEMENTATION.md` §5.4).
3. Pick a dated build key from `https://build-metadata.protomaps.dev/builds.json`, download it (`curl -C - -o planet.pmtiles https://build.protomaps.com/<YYYYMMDD>.pmtiles`), and check its md5 against the listed `md5sum`.
4. Upload under the build's dated prefix, with an R2 API token that can write to the bucket:
   ```
   aws s3 cp planet.pmtiles s3://holdmytrack-basemap/<YYYYMMDD>/basemap/basemap.pmtiles --endpoint-url https://<account-id>.r2.cloudflarestorage.com
   aws s3 cp --recursive apps/web/public/basemap/fonts s3://holdmytrack-basemap/<YYYYMMDD>/basemap/fonts --endpoint-url ...
   aws s3 cp --recursive apps/web/public/basemap/sprites s3://holdmytrack-basemap/<YYYYMMDD>/basemap/sprites --endpoint-url ...
   ```
5. Set `VITE_BASEMAP_ORIGIN` in `.env.prod` to the public origin plus that prefix (e.g. `https://pub-xxxx.r2.dev/20260922`), with no trailing slash. It's a *build*-time value for the web bundle, and `compose.prod.yml` also passes it to `api` as `BASEMAP_ORIGIN` for the style document native clients fetch. Changing it later needs `docker compose -f compose.prod.yml build web` and an `up -d`, not just a restart.

Moving to a newer build repeats steps 3–5 under a new prefix. The old prefix stays readable until you delete it, so tabs already open keep working.

A deployment without R2 can serve the archive same-origin instead: leave `VITE_BASEMAP_ORIGIN` empty and uncomment `compose.prod.yml`'s `web` volume line, pointing it at the archive on the host. That needs the whole archive on the VPS's own disk.

## 6. Bring it up

```
GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d --build
```

`GIT_SHA` is a per-invocation value, not deployment config — it isn't stored in `.env.prod`. It's baked into both the `api` and `web` images (`/healthz`'s `version` field and the frontend bundle, respectively) so a browser tab left open across a deploy can detect it's running stale — see `apps/web/src/ui/VersionBanner.tsx`. Omitting it still works (defaults to `unknown`), just without that detection.

This builds all four images, runs `migrate` once (api/worker wait for it to finish, exactly like the dev stack's own `migrate` service), and starts `db`, `api`, `worker`, and `web` (Caddy). Caddy requests its Let's Encrypt certificate for `DOMAIN` on first start — watch `docker compose -f compose.prod.yml logs web` if it doesn't come up within a minute or two.

On a new (or recreated) database, seed it once `api` is up — `migrate` creates the Demo Customer's account row but none of its activities, and nothing in `up` runs these:

```
docker compose -f compose.prod.yml --env-file .env.prod run --rm api seed-admin-boundaries
docker compose -f compose.prod.yml --env-file .env.prod run --rm api seed-demo-customer
```

Skip them and "Try it now" opens an empty demo account (`SPEC.md` FR-2.2), and Fog/Heatmap's Country/Region zoom tiers have no boundaries to draw. Both are idempotent — safe to re-run on a later deploy, they skip whatever is already loaded — so running them after every deploy is harmless, just unnecessary. Boundaries first, so the demo's activities are matched to countries/regions as they're ingested (`docs/DEVELOPMENT.md`'s "Seeding a fresh database" has the detail).

## 7. Maintenance mode

DigitalOcean (and most VPS providers) have no Droplet-level maintenance toggle, so this lives in the app stack instead. Before a deploy that touches migrations or involves manual DB work — i.e. before step 6's `up -d --build` — put the site into maintenance mode so visitors see a friendly page instead of Caddy's raw `502`s while `api` is mid-restart:

```
./scripts/maintenance.sh on
GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d --build
./scripts/maintenance.sh off
```

`maintenance.sh on`/`off` flip `MAINTENANCE_MODE` in `.env.prod` and recreate only the `web` (Caddy) container — a couple of seconds, no rebuild. `/healthz` is deliberately exempt from maintenance mode (see `apps/web/docker/Caddyfile`), so `curl https://<your-domain>/healthz` still reflects `api`/`db`'s real status; wait for it to return `200` before running `maintenance.sh off`. `./scripts/maintenance.sh status` prints the current value.

## 8. Verify

- `curl https://<your-domain>/healthz` should return `200`.
- Open the domain in a browser, sign up, upload a `.gpx` file, confirm it appears on the map — this exercises the full path: Caddy → `api` → Postgres → R2 (raw payload) → `worker` → R2 (fog/heatmap tiles) → back through Caddy to the browser.
- If Google sign-in is configured, click "Continue with Google" and confirm you land on the map signed in. A `redirect_uri_mismatch` page from Google means the authorized redirect URI in step 4 doesn't match `APP_BASE_URL` + `/v1/auth/google/callback` exactly; landing back on the sign-in screen with "Couldn't sign in with Google" means the callback failed, and `docker compose -f compose.prod.yml logs api | grep "google sign-in"` says why.
- If sign-in silently fails (redirected straight back to the sign-in screen after submitting), the most likely cause is `APP_BASE_URL` not actually being `https://` while the browser is on a plain `http://` connection, or vice versa — see step 4's note on the `Secure` cookie flag.

## What this doesn't cover

Per `docs/ROADMAP.md`, still open beyond this minimal setup: backups and a restore drill, host hardening, log rotation and monitoring (its "Production deployment" section), per-user quotas and rate limits with a spend cap (Phase 5), and the compliance work (DPIA, EU-region hosting — Phase 6) a genuine public launch needs regardless of how small the deployment is. This document gets you to "it's live," not to "it's ready for the public."