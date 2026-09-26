# Roadmap

Last updated: 2026-09-26.

## How to read this document

This is the master checklist from "what exists today" to "the full product `VISION.md` describes" — every remaining piece of work, broken into steps small enough to pick up and finish independently. It does not restate design detail already written down elsewhere:

- **`VISION.md`** is the authority on *why* and *in what order* (§5's phases, the funding model, the ingest-path strategy); its phase numbers match this file's.
- **`SPEC.md`** is the authority on *what's actually shipped*, feature by feature, with preconditions/inputs/outputs/error cases (§1.2 states scope precisely).
- **`IMPLEMENTATION.md`** is the authority on *how* each shipped piece works, and carries most of the unbuilt pieces' own design already worked out (schema, API shape, algorithm) — this document points at that design rather than re-deriving it.
- **`AGENTS.md`** is repository orientation — which document to read for what, not a status narrative of its own; `SPEC.md`/`IMPLEMENTATION.md` are where "what's built, mapped to actual files" actually lives.
- **`KNOWN_ISSUES.md`** is the opposite direction from this file: currently-open defects in already-shipped functionality, not planned work. A bug found while building something on this list belongs there, not here.

Checkboxes are the source of truth for progress; re-check them against the three docs above rather than trusting this file's memory of itself if it's been a while. A step that names a file or table already assumes the reader will open the referenced §/FR for the real detail. Once every checkbox in a subsection is checked, delete the subsection rather than leave it as a completed record — its design and rationale belong in `SPEC.md`/`IMPLEMENTATION.md` by the time it ships, not here. A partially-done section stays as-is until its own last checkbox is checked.

---

## Phase 1 — MVP

**Shipped and deployable.** Every feature in `SPEC.md`'s FR-1 through FR-11 — auth and account management, the no-signup demo, activity upload/ingestion (file, `.zip`, Google Takeout), Normal/Fog of War/Heatmap map modes with colored zone segments and high-res export, the Activities panel and its filters, track editing, Private locations, the date-range picker, the per-account activity graph, per-activity pace/heart-rate, distance trends, the public About and Help pages, and the Donate link — is built and documented there; not re-enumerated here.

### Production deployment — a sandbox is live at `holdmytrack.com`, not yet Production

**`https://holdmytrack.com` is up and serving the real stack** — but deliberately not called "Production" yet. It's a 1 vCPU / 2 GB RAM / 50 GB disk VPS with zero traffic that has never been announced anywhere. Right now it's a sandbox for the operator's own personal use (their own account, their own synced history) — real dogfooding, not a public launch, so Phase 6's compliance gates don't bind yet since no other person's data is on it. It becomes "Production" once the remaining items below are actually true of it.

- [x] `compose.prod.yml` + `apps/web/Dockerfile`'s `build`/`serve` stages + `apps/web/docker/Caddyfile` — a minimal single-VPS topology (Postgres+PostGIS, `api`, `worker`, Caddy), verified to build and validate.
- [x] TLS/HTTPS readiness — the session cookie's `Secure` flag is derived from `APP_BASE_URL`; exercised live at `https://holdmytrack.com`.
- [x] A VPS and domain are provisioned and reachable (`holdmytrack.com`, DNS on Cloudflare) — the box itself is real, even though it's sandbox-sized, not production-sized.
- [x] Move the sandbox to `holdmytrack.com` — domain on Cloudflare DNS, `APP_BASE_URL`/`DOMAIN` switched, redeployed from an empty database into a new `holdmytrack-data` bucket (the old sandbox data did not carry over); `www.holdmytrack.com`, `freefitmap.com` and `www.freefitmap.com` redirect via `REDIRECT_DOMAINS` (`apps/web/docker/Caddyfile`).
- [x] Resolve the on-box build constraint — resized to 50 GB disk (2026-09-22), which together with `58e2e60`'s single shared server-image build is enough for `docs/DEPLOY.md`'s in-place `up -d --build` to actually run; no need for the off-box-build-and-ship alternative.
- [x] `docs/DEPLOY.md` §8's verification holds on this box — `/healthz` answers `ok` with the deployed build's SHA, and real Health Connect history synced since the move shows on the map, which goes through the whole path: `api` saves each raw payload to the `holdmytrack-data` R2 bucket, then `worker` processes it and writes fog/heatmap tiles back to R2.
- [x] CDN in front of the basemap `.pmtiles` archive and tile endpoints (`VISION.md` §4.3) — the planet archive, fonts and sprites are served through Cloudflare from the public R2 bucket's custom domain `tiles.holdmytrack.com` (`IMPLEMENTATION.md` §5.4 covers what the free plan does and doesn't edge-cache).
- [ ] Backups (Postgres, object storage) and a restore drill — **the most urgent of these gaps now that real personal data (synced Health Connect history) is starting to land on this box**, not just disposable dev fixtures. Also the gate for auto-deploy on merge: CI (`.github/workflows/ci.yml`) deliberately only checks, since deploying every merge onto the one uncopied copy of real synced health data, with no restore path if a bad deploy corrupts something, is a bigger risk than the manual deploy step it would replace.
- [ ] Host hardening — a firewall allowing only 22/80/443, key-only SSH with password login disabled, unattended security updates, and `.env.prod` readable only by the deploying user.
- [ ] Bound Docker's container logs — the default `json-file` driver never rotates, so `api`/`worker`/Caddy logs grow without limit on a 50 GB disk; set `max-size`/`max-file` in `/etc/docker/daemon.json` or per service in `compose.prod.yml`.
- [ ] Basic monitoring/alerting — start with an external uptime check on `/healthz` (the cheapest signal, and what `scripts/maintenance.sh` already polls), then error rate and ingest queue depth. Cost-per-user is Phase 5's own measurement item, not an ops alert.
- [ ] Size up from sandbox hardware once real traffic is expected — 1 vCPU / 2 GB RAM is good enough for real pre-release testing, not sized for this being called Production. RAM is the real constraint: it's below `docs/DEPLOY.md`'s recommended 4 GB, and Postgres, the Go server and a Takeout import running at once can trigger the kernel OOM killer, which may kill Postgres. Until then, swap is the cheap stopgap.

### Pre-launch validation — gates any public launch, regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after The app side is built (`IMPLEMENTATION.md` §4.16); what's left: create the `holdmytrack` collective on opencollective.com and apply to Open Source Collective as fiscal host, then once approved set the slug — `apps/web/src/funding.ts`'s `OPEN_COLLECTIVE_SLUG` and `services/server/internal/web/web.go`'s `OpenCollectiveSlug` — and replace the About page template's "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free forever, funded by users" as credible before building further.

### Sign in with Facebook — built, not live

Sign in with Facebook is built (`SPEC.md` FR-1.10) and the Meta app exists, but Meta won't publish an app until its business portfolio passes Business Verification (`docs/DEPLOY.md` §4). Until then `holdmytrack.com` runs with `FACEBOOK_APP_ID` empty, so it shows no Facebook button. This isn't a launch gate: Google and email sign-in cover everyone.

- [ ] Get a business document for the "Holdmytrack" business portfolio. Meta accepts one of: an IRS 147C letter (EIN confirmation), a business bank statement, a business tax document, or a "Doing Business As" (DBA) filing. The name on it must match the portfolio's. A sole-proprietor EIN with "HoldMyTrack" as its trade name, or a county/state DBA filing, are the cheapest routes; so may be whatever legal standing the Open Collective fiscal host above gives the project. Check the legal and tax implications before filing just for this.
- [ ] Complete Business Verification with it, connect the Meta app to the verified portfolio and publish it (`docs/DEPLOY.md` §4 step 6), then set `FACEBOOK_APP_ID`/`FACEBOOK_APP_SECRET` in the server's `.env.prod` and recreate `api`.

---

## Phase 2 — Mobile

Native apps whose core job is exporting device-recorded health data to HoldMyTrack (Path 2 on-device sync, `docs/adr/0001-three-independent-ingest-paths.md`) — reading the platform's own health store rather than a cloud API. Needed no licensing gate the way Phase 4's cloud connectors do: HealthKit/Health Connect access itself isn't in question, only how much of it. Android and iOS are separate builds against separate platform constraints (Health Connect vs. HealthKit, Play Store vs. App Store), tracked as two independent subsections below rather than one interleaved list — though iOS's own Path 2 sync contract (payload shape, sync-cursor semantics) inherits directly from whatever Android already settled.

### Android

- [ ] Confirm the Samsung Health Connect route-geometry limitation empirically, not just from documentation (`VISION.md` §4.1) — Samsung's own developer docs already state `EXERCISE_ROUTE` cannot be read via Health Connect; this is double-checking in case reality is better than documented, not an open question blocking the build.
- [x] Android app: Health Connect sync, foreground-only (`READ_EXERCISE_ROUTES` can't be requested programmatically, and background route reads return `ConsentRequired` even with "Always allow" granted — a platform constraint, not an implementation shortcut). Samsung Galaxy Watch is unsupported — it never exposes route geometry, and HoldMyTrack only ingests activities that have one (the confirmation item above is about verifying that limitation firsthand, not about whether sync itself works). Built first as planned, so the Path 2 sync contract (payload shape, sync-cursor semantics) was designed against the harder platform's constraints, ready for iOS to inherit. `apps/android/docs/ROADMAP.md` carries this app's remaining work (UI design freeze, Play Store compliance) — this root item tracks only "does Path 2 sync itself work end to end," which it now does (`docs/SPEC.md` FR-3.6).
- [x] In-app GPS recording, Android half — a convenience capture for casual, watch-free activities (a road trip, a dog walk), not a fitness-tracker replacement: GPS only, no sensor data, no training metrics (`VISION.md` §4.1, §1.1). Submits directly through the existing ingest pipeline once a recording stops, reusing the sync endpoint's payload shape rather than opening a new one ([ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)) — no new server-side path, just a new `source` value. `apps/android/docs/ROADMAP.md` Phase 7 carries the detail; `docs/SPEC.md` FR-3.8 is the behavior spec.

### iOS

- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry — the stronger of the two on-device paths, and the one that inherits the payload and sync-cursor design Android settles.
- [ ] In-app GPS recording, iOS half — inherits the Android build's wire shape and `source` convention once the iOS app itself exists (see the iOS Path 2 item above, which this depends on).

---

## Phase 3 — Finalized design + mobile browser support

The shipped UI so far is functional scaffolding, not a finished product. Partly addressed since this phase was written: icons are one Lucide set (`IMPLEMENTATION.md` §4.17), type is Inter with Fraunces for headings, and colors are `--fm-*` custom properties. Spacing, radius, type, weight and elevation are token scales too (`IMPLEMENTATION.md` §4.18). What's left is almost no animation — 5 `transition:`/`animation:`/`@keyframes` occurrences in the ~3,600-line `index.css` — and real-device mobile browser support. This phase is the pass that finishes it, across both desktop and mobile, ending in an explicit design freeze: "this is how it will look — no more changes."

- [x] A real icon set — Lucide (`lucide-react`) replaces every hand-drawn inline SVG icon and text-glyph caret on the web (`IMPLEMENTATION.md` §4.17); Android adopts the same set in its own Phase 5.
- [x] Real typography — Inter for text, Fraunces for headings and the wordmark, with Source Serif 4 for Russian headings since Fraunces has no Cyrillic (`--fm-font-sans`/`--fm-font-serif`, loaded from Google Fonts in `index.html`).
- [x] An actual design system — the `--fm-*` palette plus spacing, radius, type, weight and elevation scales in one shared `tokens.css` (served by the Go server, loaded by every page and the map app), used by every declaration except a few deliberate literals (`IMPLEMENTATION.md` §4.18). The one literal color left in use is `#fff` (12 uses), plus two single-use colors.
- [x] Server-rendered pages sharing one header, React kept for the map page ([ADR-0012](adr/0012-server-rendered-pages-react-for-the-map.md), `IMPLEMENTATION.md` §4.19), in shippable steps:
  - [x] Import moves out of the header into the Activities panel's Sync tab (`IMPLEMENTATION.md` §4.0.1).
  - [x] The rendering foundation (`internal/web`, the shared header, Sign out as a same-origin-checked form) with About, Help and Contacts as its first pages (`IMPLEMENTATION.md` §4.14).
  - [x] Sign-in, sign-up, password reset, email verification and demo start as pages, replacing `AuthGate.tsx`; email links move to `/verify?token=`/`/reset?token=`, with the old `/?…_token=` forms still redirected (`IMPLEMENTATION.md` §4.19).
  - [x] The map page served by Go with the shared header, replacing `Header.tsx`/`UserMenu.tsx`/`InfoMenu.tsx`/`DonateButton.tsx`; Export becomes a map control; Caddy sends everything but static files to Go; Profile and Settings get URLs (`/profile`, `/settings`) as views in the same shell (`IMPLEMENTATION.md` §4.19). The first-run gate stays in React until Settings is a page.
  - [x] Settings as a page (`/settings`), a plain form with native selects; the first-run gate moves server-side with it (`IMPLEMENTATION.md` §4.12).
  - [x] Profile as a page (`/profile`), the year grids and trends rendered server-side (`IMPLEMENTATION.md` §4.8).
- [x] Localization — English and Russian across the server's pages, emails and messages, the map app and Android, with a Language setting that falls back to the browser's ([ADR-0014](adr/0014-localization.md), `IMPLEMENTATION.md` §4.21, `SPEC.md` FR-13). Left: running the Android app in Russian on a real device, a native speaker's review of the Russian, and `KNOWN_ISSUES.md`'s two entries (a Cyrillic heading font, and the server messages still in English).
- [ ] An animation/transition pass — micro-interactions (hover, focus, panel open/close, loading states) that are currently almost entirely absent.
- [ ] Mobile browser support, folded into this same pass rather than treated separately — the phone layout exists (`index.css`'s `@media (max-width: 768px)` layer, `IMPLEMENTATION.md` §5.9, `SPEC.md` §17) and was reported directly as unusable on a real phone; four causes emulation can't show have since been fixed (§5.9's **Real-device fixes**), but nothing has been checked on an actual device yet.
  - [ ] Walk the core flows on a real iPhone (Safari) and Android phone (Chrome) — sign in, the three map modes, tap a track, expand and collapse the sheet, the date slider, edit an activity's name — and record any symptom concretely (device, browser, screen, what happened), not as "unusable".
- [ ] Design freeze: once this pass lands, declare the visual design final and communicate it as such — the explicit milestone this phase produces, not an open-ended polish effort.

This pass is where HoldMyTrack's palette, typography and icon set come from for the product as a whole, not for the web alone. The Android app carries only a provisional theme (the web's current palette in Material 3) and its own phase for landing this output on the platform (`apps/android/docs/ROADMAP.md`, Phase 5) — it inherits the freeze rather than deciding a second visual design, since two clients that each invented their own would not read as one product.

---

## Phase 4 — Cloud sources + Exploration scoring

Connecting the app to third-party services, and the explorer-tile scoring work that benefits from the broader activity history that unlocks.

### Prerequisites — gate the specific connectors below, not this phase's other work

- [ ] Get Garmin's Connect Developer Program licence position in writing for a free, donation-funded service (`VISION.md` §4.1, §8.3) — the one item that can impose a fixed cost this funding model cannot absorb.
- [ ] File the Wahoo partner-API application (lead time, not cost, is the risk).
- [ ] File the COROS partner-API application.

### Path 1 — cloud connectors (gated on the prerequisites above)

- [ ] Generic OAuth connection scaffolding — `connections` table already exists (`migrations/0001_init.sql` §3.2); build the authorize/callback/token-refresh flow once, provider-agnostic, before any specific provider.
- [ ] Garmin connector (after the licence prerequisite is settled).
- [ ] Wahoo connector (after partner approval).
- [ ] COROS connector (after partner approval).
- [ ] Deauthorization deletion for each connector as it ships, not after — Garmin/Wahoo/COROS contractually require it (§7).
- [ ] These connectors are the one part of the Activities panel's Sync tab on the web (`docs/IMPLEMENTATION.md` §4.0.1) that's actually triggerable from the tab itself — connect/disconnect, and (once token-refresh runs on a schedule) a last-synced/status summary alongside Health Connect/GPS Logger's own read-only rows there. Sync stayed read-only-only through Phase 2 specifically because neither on-device source can be triggered from the web; a cloud connector can.

### Explorer-tile gamification

- [ ] `user_tiles` table exists but nothing writes or reads it yet — populating it at ingest (`IMPLEMENTATION.md` §4.1 step 4), scoring queries (total tiles, max square, max connected cluster, per-region coverage %, `IMPLEMENTATION.md` §4.4) and a UI surface for them (a natural fit on the Profile page, alongside the activity grid).

---

## Phase 5 — Cost control

An engineering requirement, can land alongside any of the above (`IMPLEMENTATION.md` §5.7).

- [ ] Retention/dormancy policy: tier `activity_streams` to cold storage or drop it after N months of inactivity (`users.last_seen_at`), keeping summaries and fog rasters so the map still renders; warn by email first; recoverable by re-upload.
- [ ] Raw payload expiry schedule (object storage) — note this caps how far back a `reprivacy` job (a Private location change) can reach; document the tradeoff wherever it's implemented.
- [ ] Per-user quotas (activity count, total points) — bounds one pathological account's cost, not a monetization lever.
- [ ] Rate limits on upload, export, and tile requests (the auth endpoints already have `fixedWindowLimiter` — reuse it), plus a CDN/object-store spend cap.
- [ ] Cost-per-active-user measurement from day one — the number that decides whether `VISION.md` §6's funding model actually works.

---

## Phase 6 — Compliance

Non-negotiable, GDPR Art. 9 special-category data (`VISION.md` §7).

- [ ] DPIA before any public launch.
- [ ] EU-region hosting for EU users.
- [ ] Working data export and deletion endpoints (account deletion doesn't exist yet — confirm and build if missing). Until it exists, Help's `#delete-account` section asks people to email for deletion, and it is also the Meta app's data-deletion instructions URL (`IMPLEMENTATION.md` §4.22) — update both when this ships. **Naming collision to resolve when this is built:** the map screen's own "Export" header control (FR-4.10) already owns that word for a completely different action (a framed PNG capture) — whoever builds this should either rename FR-4.10's control (candidates raised: "Share," "Save") or pick a different label here, so the two don't collide.
- [ ] Health Connect data-type declarations in the Play Console, scoped to only what's actually used (Phase 2 builds the app; the declaration work belongs here).
- [ ] Confirm we don't need a cookie consent banner — as of this writing the web client sets exactly one cookie (`holdmytrack_session`: `HttpOnly`, `SameSite=Lax`, `Secure` under HTTPS, no `localStorage`/analytics/tracking anywhere in `apps/web`), which should fall under the ePrivacy Directive Art. 5(3) "strictly necessary" exemption — no consent required, only a plain-language disclosure in the privacy policy. Re-check this conclusion at launch time (cookie/analytics usage can drift) and again the day anything non-essential (analytics, an ad pixel, marketing tracking) is added, since that would flip the answer.

---

## Phase 7 — Social (deliberately not committed)

`VISION.md` §5.7/§5.8 is explicit that this should not be scheduled, let alone built, until the funding base can absorb the moderation and trust-and-safety staffing it requires — a fog map is a precise record of where someone lives, and a social graph on top of that is a threat-model change, not a feature. No steps are listed here on purpose; the first real step is revisiting §6's funding numbers, not writing code.

---

## Ongoing, not phase-bound

- [ ] Re-measure the funding-model assumptions (§4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold.