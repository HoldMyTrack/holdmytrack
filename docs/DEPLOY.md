# Deploying FitMap — minimal single-VPS setup

The smallest deployment that's actually production-shaped: one small VPS running `compose.prod.yml` (Postgres+PostGIS, `api`, `worker`, and Caddy in front of the built frontend), plus Cloudflare R2 for object storage. See `docs/VISION.md` §4.3 for the cost model this is built around, and `docs/ROADMAP.md`'s "Production deployment" section for what's still open beyond this (backups, monitoring, spend caps, DPIA — this document only covers getting a working deployment live, not everything a real public launch needs).

## 1. Provision the VPS

Any small Docker-capable VPS works — 2 vCPU / 4 GB RAM is comfortable headroom for Postgres+PostGIS, `api`, `worker`, and Caddy together at low traffic. Install Docker and the Compose plugin (`docker compose version` should work), then clone this repo onto it.

## 2. Create the Cloudflare R2 bucket

1. Create an R2 bucket in the Cloudflare dashboard.
2. Create an R2 API token scoped to that bucket (Account → R2 → Manage API Tokens) — this gives you the access key, secret key, and the account-specific S3-compatible endpoint (`https://<account-id>.r2.cloudflarestorage.com`). None of this is the same as a Cloudflare account-wide API token.
3. No code change is needed for this — `internal/storage/storage.go` already talks to any S3-compatible endpoint via `minio-go`; local dev's MinIO container is a stand-in for exactly this.

## 3. Point DNS at the server

Add an `A` record for your domain (or a subdomain, e.g. `app.example.com`) pointing at the VPS's public IP. Caddy (step 5) needs this to resolve correctly *before* it first starts, or its automatic Let's Encrypt certificate request will fail.

## 4. Fill in the production env file

```
cp .env.prod.example .env.prod
```

Fill in every value — see that file's own comments for what each one means and why it has no default (unlike dev's `.env.example`, nothing here is safe to leave as a placeholder). `APP_BASE_URL` and `DOMAIN` both need the real domain from step 3; get `APP_BASE_URL`'s `https://` scheme right — `auth.go`'s session cookie derives its `Secure` flag from it, and password-reset emails link back into it.

## 5. Basemap: pick one

The basemap archive (`.pmtiles`) is deliberately excluded from every Docker build context (`apps/web/.dockerignore`), so the `web` image never has it baked in. Two options:

**A — bind-mount it (simplest, no rebuild to update).** `apps/web/scripts/build-basemap.sh` is what cuts dev's small Ohio extract from the remote Protomaps planet build via `pmtiles extract`; the same script with `REGION`/`BBOX` widened (up to the whole planet, at real `.pmtiles` size — `VISION.md` §4.3 prices this at ~138 GB) is how you'd cut whatever coverage you actually want. Put the result somewhere on the VPS (e.g. `/srv/fitmap/basemap/planet.pmtiles`) and uncomment `compose.prod.yml`'s `web` service's volume line, pointing at that path. Leave `VITE_BASEMAP_ORIGIN` empty in `.env.prod`.

**B — host it on R2/a CDN (matches `VISION.md`'s actual intended production architecture — worth doing once tile-read costs matter enough to want a CDN in front of it).** Upload the archive plus `apps/web/public/basemap/fonts/` and `.../sprites/` to a public R2 bucket (or a CDN in front of one), then set `VITE_BASEMAP_ORIGIN` in `.env.prod` to that bucket's/CDN's origin (e.g. `https://basemap.example.com`) — no trailing slash. This is a *build*-time value; changing it later needs `docker compose -f compose.prod.yml build web`, not just a restart.

Start with A. Move to B when tile-read volume actually justifies it.

## 6. Bring it up

```
GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d --build
```

`GIT_SHA` is a per-invocation value, not deployment config — it isn't stored in `.env.prod`. It's baked into both the `api` and `web` images (`/healthz`'s `version` field and the frontend bundle, respectively) so a browser tab left open across a deploy can detect it's running stale — see `apps/web/src/ui/VersionBanner.tsx`. Omitting it still works (defaults to `unknown`), just without that detection.

This builds all four images, runs `migrate` once (api/worker wait for it to finish, exactly like the dev stack's own `migrate` service), and starts `db`, `api`, `worker`, and `web` (Caddy). Caddy requests its Let's Encrypt certificate for `DOMAIN` on first start — watch `docker compose -f compose.prod.yml logs web` if it doesn't come up within a minute or two.

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
- If sign-in silently fails (redirected straight back to the sign-in screen after submitting), the most likely cause is `APP_BASE_URL` not actually being `https://` while the browser is on a plain `http://` connection, or vice versa — see step 4's note on the `Secure` cookie flag.

## What this doesn't cover

Per `docs/ROADMAP.md`'s "Production deployment" checklist, still open beyond this minimal setup: backups and a restore drill, per-user quotas and rate limits with a spend cap, cost-per-user monitoring, and the compliance work (DPIA, EU-region hosting) a genuine public launch needs regardless of how small the deployment is. This document gets you to "it's live," not to "it's ready for the public."