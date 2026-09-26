# Deploying HoldMyTrack — minimal single-VPS setup

The smallest deployment that's actually production-shaped: one small VPS running `compose.prod.yml` (Postgres+PostGIS, `api`, `worker`, and Caddy in front of the built frontend), plus Cloudflare R2 for object storage. See `docs/VISION.md` §4.3 for the cost model this is built around, and `docs/ROADMAP.md`'s "Production deployment" section for what's still open on the operational side (backups, host hardening, monitoring); spend caps and the compliance work (DPIA, EU-region hosting) are that file's Phases 5 and 6. This document only covers getting a working deployment live, not everything a real public launch needs.

## 1. Provision the VPS

Any small Docker-capable VPS works — 2 vCPU / 4 GB RAM is comfortable headroom for Postgres+PostGIS, `api`, `worker`, and Caddy together at low traffic. Install Docker and the Compose plugin (`docker compose version` should work), then clone this repo onto it.

## 2. Create the Cloudflare R2 bucket

1. Create an R2 bucket in the Cloudflare dashboard.
2. Create an R2 API token scoped to that bucket (Account → R2 → Manage API Tokens) — this gives you the access key, secret key, and the account-specific S3-compatible endpoint (`https://<account-id>.r2.cloudflarestorage.com`). None of this is the same as a Cloudflare account-wide API token.
3. No code change is needed for this — `internal/storage/storage.go` already talks to any S3-compatible endpoint via `minio-go`; local dev's RustFS container is a stand-in for exactly this.

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

**Sign in with Facebook (optional).** Leave `FACEBOOK_APP_ID` empty to run without it. Facebook never links to an existing account by email, and a new account it creates verifies its email by mail (`SPEC.md` FR-1.10, ADR-0015), so SMTP must work before you turn this on. Meta renames dashboard labels from time to time; if one below doesn't match, search the dashboard for the setting.

1. At developers.facebook.com, log in and register as a developer (Get Started), then **My Apps → Create App**: name HoldMyTrack, a contact email, and the use case "Authenticate and request data from users with Facebook Login". A business portfolio isn't needed.
2. Under the use case's **Customize**, make sure the `email` permission is added next to `public_profile`. Both have standard access and need no App Review.
3. In **Facebook Login → Settings**, keep Client OAuth login, Web OAuth login and Enforce HTTPS on, and add `https://<your-domain>/v1/auth/facebook/callback` to **Valid OAuth Redirect URIs**, exactly `APP_BASE_URL` plus that path. For local dev also add `http://localhost:<API_PORT>/v1/auth/facebook/callback` (allowed over http while the app is in Development mode).
4. In **App settings → Basic**, copy the **App ID** and, after **Show** and re-entering your Facebook password, the **App secret** — treat it as a password: `.env.prod` only, never a committed file; **Reset** there if it leaks. On the same page, fill in App domains (the domain from step 3), a Privacy Policy URL, "Data deletion instructions URL" set to `https://<your-domain>/help#delete-account`, an app icon (1024×1024) and a category, and save.
5. Put them into `.env.prod` as `FACEBOOK_APP_ID`/`FACEBOOK_APP_SECRET`, leave `FACEBOOK_REDIRECT_URL` empty, and recreate `api` (`up -d`).
6. While the app is in Development mode, only people with a role on it (**App roles → Roles**) can sign in — test with one. Then switch it to **Live** (Publish on the app's dashboard); Meta checks the fields from step 4 first.

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

On a new (or recreated) database, seed it once `api` is up — `migrate` creates the Demo Customer's account row but none of its activities, and nothing in `up` runs these. An **existing** database needs the same step once when a deploy first brings in a seed it has never had: a database created before the Country/Region zoom tiers shipped has no boundary rows until `seed-admin-boundaries` runs, and nothing fails loudly — the tiers just render blank:

```
docker compose -f compose.prod.yml --env-file .env.prod run --rm api seed-admin-boundaries
docker compose -f compose.prod.yml --env-file .env.prod run --rm api seed-demo-customer
```

Skip them and "Try it now" opens an empty demo account (`SPEC.md` FR-2.2), and Fog/Heatmap's Country/Region zoom tiers have no boundaries to draw. Both are idempotent — safe to re-run on a later deploy, they skip whatever is already loaded — so running them after every deploy is harmless, just unnecessary. Boundaries first, so the demo's activities are matched to countries/regions as they're ingested (`docs/DEVELOPMENT.md`'s "Seeding a fresh database" has the detail).

To open the admin panel (`/admin`, `SPEC.md` FR-12), make your own account an admin. It has to exist first, so sign up on the site, then:

```
docker compose -f compose.prod.yml --env-file .env.prod run --rm api set-admin you@example.com true
```

`false` in place of `true` revokes it. This is the only way to grant or revoke admin; nothing on the web can.

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
- Switch to Fog, then zoom out below city zoom: any country the account has an activity in should render clear against the veil. If the whole world stays uniformly fogged (and Heatmap shows no country/region highlight either), `admin_countries`/`admin_regions` are empty — run step 6's `seed-admin-boundaries`, then hard-refresh; the tiles are queried live, so nothing else needs rebuilding.
- If Google sign-in is configured, click "Continue with Google" and confirm you land on the map signed in. A `redirect_uri_mismatch` page from Google means the authorized redirect URI in step 4 doesn't match `APP_BASE_URL` + `/v1/auth/google/callback` exactly; landing back on the sign-in screen with "Couldn't sign in with Google" means the callback failed, and `docker compose -f compose.prod.yml logs api | grep "google sign-in"` says why.
- If sign-in silently fails (redirected straight back to the sign-in screen after submitting), the most likely cause is `APP_BASE_URL` not actually being `https://` while the browser is on a plain `http://` connection, or vice versa — see step 4's note on the `Secure` cookie flag.

## 9. Before upgrading the Postgres image

The server passes each account's stored time zone straight to Postgres (`AT TIME ZONE users.timezone`, every per-day query), and the `postgis/postgis` image reads time zones from the operating system's own files (it's built `--with-system-tzdata`). Today's `16-3.4` image is Debian 11, whose files still carry the old names IANA has since replaced, like `Asia/Calcutta` for `Asia/Kolkata` and `Europe/Kiev` for `Europe/Kyiv`. Debian 13 moved those old names into a separate `tzdata-legacy` package that isn't installed by default. On an image built on it, any account still storing an old name would make those queries fail with an error.

New accounts can't get one: the server stores every time zone under its current name (`IMPLEMENTATION.md` §4.12). But an account that signed up before that change (2026-09-25) keeps the old spelling its browser reported until it next saves Settings. So before moving `db` to an image on Debian 13 or later, rewrite those, while the old image is still running:

```bash
docker compose -f compose.prod.yml --env-file .env.prod exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

```sql
UPDATE users u SET timezone = r.current
FROM (VALUES
  ('Africa/Asmera', 'Africa/Asmara'),
  ('America/Buenos_Aires', 'America/Argentina/Buenos_Aires'),
  ('America/Catamarca', 'America/Argentina/Catamarca'),
  ('America/Cordoba', 'America/Argentina/Cordoba'),
  ('America/Godthab', 'America/Nuuk'),
  ('America/Indianapolis', 'America/Indiana/Indianapolis'),
  ('America/Jujuy', 'America/Argentina/Jujuy'),
  ('America/Knox_IN', 'America/Indiana/Knox'),
  ('America/Louisville', 'America/Kentucky/Louisville'),
  ('America/Mendoza', 'America/Argentina/Mendoza'),
  ('America/Virgin', 'America/St_Thomas'),
  ('Asia/Ashkhabad', 'Asia/Ashgabat'),
  ('Asia/Calcutta', 'Asia/Kolkata'),
  ('Asia/Chungking', 'Asia/Chongqing'),
  ('Asia/Dacca', 'Asia/Dhaka'),
  ('Asia/Istanbul', 'Europe/Istanbul'),
  ('Asia/Katmandu', 'Asia/Kathmandu'),
  ('Asia/Macao', 'Asia/Macau'),
  ('Asia/Rangoon', 'Asia/Yangon'),
  ('Asia/Saigon', 'Asia/Ho_Chi_Minh'),
  ('Asia/Thimbu', 'Asia/Thimphu'),
  ('Asia/Ujung_Pandang', 'Asia/Makassar'),
  ('Asia/Ulan_Bator', 'Asia/Ulaanbaatar'),
  ('Atlantic/Faeroe', 'Atlantic/Faroe'),
  ('Europe/Kiev', 'Europe/Kyiv'),
  ('Europe/Nicosia', 'Asia/Nicosia'),
  ('HST', 'Pacific/Honolulu'),
  ('Pacific/Ponape', 'Pacific/Pohnpei'),
  ('Pacific/Samoa', 'Pacific/Pago_Pago'),
  ('Pacific/Truk', 'Pacific/Chuuk')
) AS r(old, current)
WHERE u.timezone = r.old;
```

That's the rename table in `services/server/internal/web/timezones_data.go` (`timezoneRenames`, tzdata 2026d) at the time of writing. If the table has been regenerated since, rebuild the list from it. Then check that nothing is left that the new image won't know: run `SELECT DISTINCT timezone FROM users` and look each result up in the new image's `pg_timezone_names`.

## What this doesn't cover

Per `docs/ROADMAP.md`, still open beyond this minimal setup: backups and a restore drill, host hardening, log rotation and monitoring (its "Production deployment" section), per-user quotas and rate limits with a spend cap (Phase 5), and the compliance work (DPIA, EU-region hosting — Phase 6) a genuine public launch needs regardless of how small the deployment is. This document gets you to "it's live," not to "it's ready for the public."