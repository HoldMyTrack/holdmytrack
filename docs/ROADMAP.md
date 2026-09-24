# Roadmap

Last updated: 2026-09-24.

## How to read this document

This is the master checklist from "what exists today" to "the full product `VISION.md` describes" — every remaining piece of work, broken into steps small enough to pick up and finish independently. It does not restate design detail already written down elsewhere:

- **`VISION.md`** is the authority on *why* and *in what order* (§5's phases, the funding model, the ingest-path strategy) — note that this file's delivery order (Mobile, then a design-finalization pass, before Analysis+Cloud sources) currently diverges from `VISION.md` §5's stated sequence; not yet reconciled.
- **`SPEC.md`** is the authority on *what's actually shipped*, feature by feature, with preconditions/inputs/outputs/error cases (§1.2 states scope precisely).
- **`IMPLEMENTATION.md`** is the authority on *how* each shipped piece works, and carries most of the unbuilt pieces' own design already worked out (schema, API shape, algorithm) — this document points at that design rather than re-deriving it.
- **`AGENTS.md`** is repository orientation — which document to read for what, not a status narrative of its own; `SPEC.md`/`IMPLEMENTATION.md` are where "what's built, mapped to actual files" actually lives.
- **`KNOWN_ISSUES.md`** is the opposite direction from this file: currently-open defects in already-shipped functionality, not planned work. A bug found while building something on this list belongs there, not here.

Checkboxes are the source of truth for progress; re-check them against the three docs above rather than trusting this file's memory of itself if it's been a while. A step that names a file or table already assumes the reader will open the referenced §/FR for the real detail. Once every checkbox in a subsection is checked, delete the subsection rather than leave it as a completed record — its design and rationale belong in `SPEC.md`/`IMPLEMENTATION.md` by the time it ships, not here. A partially-done section stays as-is until its own last checkbox is checked.

---

## Phase 1 — MVP

**Shipped and deployable.** Every feature in `SPEC.md`'s FR-1 through FR-10 — auth and account management, the no-signup demo, activity upload/ingestion (file, `.zip`, Google Takeout), Normal/Fog of War/Heatmap map modes with colored zone segments and high-res export, the Activities panel and its filters, the date-range picker, the per-account activity graph, per-activity pace/heart-rate, distance trends, and the public About page — is built and documented there; not re-enumerated here.

### Production deployment — a sandbox is live at `holdmytrack.com`, not yet Production

**`https://holdmytrack.com` is up and serving the real stack** (moved there 2026-09-24 from the pre-rebrand `freefitmap.com` sandbox, starting from an empty database and a new `holdmytrack-data` bucket; `freefitmap.com` now 301s to it) — but deliberately not called "Production" yet. It's a 1 vCPU / 2 GB RAM / 50 GB disk VPS, resized 2026-09-22 from an earlier, disk-constrained box that couldn't build the Docker images on-box at all; the bigger disk (plus `58e2e60`'s single shared server-image build) now makes `docs/DEPLOY.md`'s in-place `up -d --build` work, so images no longer need to be built off-box and shipped in. It has zero traffic and has never been announced anywhere. Right now it's a sandbox for the operator's own personal use (their own account, their own synced history) — real dogfooding, not a public launch, so Phase 6's compliance gates don't bind yet since no other person's data is on it. It becomes "Production" once the remaining items below are actually true of it.

- [x] `compose.prod.yml` + `apps/web/Dockerfile`'s `build`/`serve` stages + `apps/web/docker/Caddyfile` — a minimal single-VPS topology (Postgres+PostGIS, `api`, `worker`, Caddy), verified to build and validate.
- [x] TLS/HTTPS readiness — the session cookie's `Secure` flag is derived from `APP_BASE_URL`; exercised live at `https://holdmytrack.com`.
- [x] A VPS and domain are provisioned and reachable (`holdmytrack.com`, DNS on Cloudflare) — the box itself is real, even though it's sandbox-sized, not production-sized.
- [x] Move the sandbox to `holdmytrack.com` — domain on Cloudflare DNS, `APP_BASE_URL`/`DOMAIN` switched, redeployed from an empty database into a new `holdmytrack-data` bucket (the old sandbox data did not carry over); `www.holdmytrack.com`, `freefitmap.com` and `www.freefitmap.com` redirect via `REDIRECT_DOMAINS` (`apps/web/docker/Caddyfile`).
- [x] Resolve the on-box build constraint — resized to 50 GB disk (2026-09-22), enough for `docs/DEPLOY.md`'s in-place build to actually run; no need for the off-box-build-and-ship alternative.
- [ ] Confirm R2 (or equivalent object storage) is actually wired in, not just Postgres — `docs/DEPLOY.md` end to end hasn't been confirmed run against this box specifically.
- [ ] Size up from sandbox hardware once real traffic is expected — 1 vCPU / 2 GB RAM is good enough for real pre-release testing, not sized for this being called Production; RAM in particular is still below `docs/DEPLOY.md`'s recommended 4 GB headroom.
- [ ] CDN in front of the basemap `.pmtiles` archive and tile endpoints (§4.3 of the business plan — this is the one basemap cost that scales with usage without one). The planet archive is now served from the public R2 bucket's custom domain `tiles.holdmytrack.com` rather than its rate-limited `r2.dev` URL, but Cloudflare's free plan doesn't edge-cache objects that large (only the small fonts/sprites get cached), so range reads into the archive still go to R2 every time — R2 has no egress fee, only per-request cost. Real edge caching means splitting the archive into cacheable pieces or a plan with a higher cacheable-size limit.
- [ ] Backups (Postgres, object storage) and a restore drill — **the most urgent of these gaps now that real personal data (synced Health Connect history) is starting to land on this box**, not just disposable dev fixtures.
- [ ] Basic monitoring/alerting (error rate, queue depth, cost-per-user from Phase 5's measurement item).

### CI on push/PR — closes the gap where `tsc`/Go tests/`make test` already exist but nothing runs them automatically

Now that `main` feeds a real, if small, live deployment (`holdmytrack.com`), a broken build or a regression slipping past manual local verification is a materially bigger risk than it was pre-deployment. `tsc --noEmit`, `make test` (the web's `verify:map`/`verify:build`), and four Go unit test files (`dedupe_test.go`, `parse_test.go`, `fit_test.go`, `mapstyle_test.go`) already exist, but nothing runs them except whoever remembers to type the command locally — there is no `.github/workflows` at all today, despite the repo already living on GitHub and already using PRs.

- [ ] A GitHub Actions workflow running on every push/PR: `tsc --noEmit`, `go test ./...`, and `make test` — all three already exist and already pass locally; this is wiring, not new test-writing.
- [ ] Fold `go test ./...` into `make test` itself (or a sibling target) while at it — right now the Go tests aren't part of any single command, automated or not, so even a careful local run before pushing can miss them.
- [ ] Deliberately not a large new unit-test-writing effort — this project's stated quality approach favors manual, live verification over unit tests (`IMPLEMENTATION.md`'s repeated "confirmed live, not just a unit test", `DEVELOPMENT.md`'s verification checklist); this item wires up checks that already exist, it doesn't change that philosophy.
- [ ] **CD (auto-deploy on merge) is explicitly out of scope here, and gated behind the Production deployment section's backups item above.** Auto-deploying every merge onto the one uncopied copy of real synced health data, with no restore path if a bad deploy corrupts something, is a bigger risk than the manual deploy step it would replace. Revisit once backups and a restore drill exist.

### Pre-launch validation — gates any public launch, regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after The app side is built (`IMPLEMENTATION.md` §4.16); what's left: create the `holdmytrack` collective on opencollective.com and apply to Open Source Collective as fiscal host, then once approved set `apps/web/src/funding.ts`'s `OPEN_COLLECTIVE_SLUG` and replace `about.html`'s "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free forever, funded by users" as credible before building further.

---

## Phase 2 — Mobile

Native apps whose core job is exporting device-recorded health data to HoldMyTrack (Path 2 on-device sync, `docs/adr/0001-three-independent-ingest-paths.md`) — reading the platform's own health store rather than a cloud API. Needed no licensing gate the way Phase 4's cloud connectors do: HealthKit/Health Connect access itself isn't in question, only how much of it. Android and iOS are separate builds against separate platform constraints (Health Connect vs. HealthKit, Play Store vs. App Store), tracked as two independent subsections below rather than one interleaved list — though iOS's own Path 2 sync contract (payload shape, sync-cursor semantics) inherits directly from whatever Android already settled.

### Android

- [ ] Confirm the Samsung Health Connect route-geometry limitation empirically, not just from documentation (`VISION.md` §4.1) — Samsung's own developer docs already state `EXERCISE_ROUTE` cannot be read via Health Connect; this is double-checking in case reality is better than documented, not an open question blocking the build.
- [x] Android app: Health Connect sync, foreground-only (`READ_EXERCISE_ROUTES` can't be requested programmatically, and background route reads return `ConsentRequired` even with "Always allow" granted — a platform constraint, not an implementation shortcut). Samsung Galaxy Watch is unsupported — it never exposes route geometry, and HoldMyTrack only ingests activities that have one (the confirmation item above is about verifying that limitation firsthand, not about whether sync itself works). Built first as planned, so the Path 2 sync contract (payload shape, sync-cursor semantics) was designed against the harder platform's constraints, ready for iOS to inherit. `apps/android/docs/ROADMAP.md` carries this app's remaining work (UI design freeze, Play Store compliance) — this root item tracks only "does Path 2 sync itself work end to end," which it now does (`docs/SPEC.md` FR-3.6).
- [x] In-app GPS recording, Android half — a convenience capture for casual, watch-free activities (a road trip, a dog walk), not a fitness-tracker replacement: GPS only, no sensor data, no training metrics (`VISION.md` §4.1, §1.1). Submits directly through the existing ingest pipeline once a recording stops, reusing the sync endpoint's payload shape rather than opening a new one ([ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)) — no new server-side path, just a new `source` value. `apps/android/docs/ROADMAP.md` Phase 7 carries the detail; `docs/SPEC.md` FR-3.8 is the behavior spec.
- [ ] Sign in with Google on Android — web has it (`docs/SPEC.md` FR-1.9, `docs/IMPLEMENTATION.md` §4.15). Android would get a Google ID token on-device through Credential Manager and post it to a new endpoint that, unlike the web's server-side code flow, must verify the token's signature against Google's JWKS (it arrives from the client, not straight from Google), then reuse `resolveGoogleUser` and return `session_token` like the other sign-in endpoints. Needs an Android OAuth client in the same Google Cloud project, tied to the app's signing-key SHA-1.

### iOS

- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry — the stronger of the two on-device paths, and the one that inherits the payload and sync-cursor design Android settles.
- [ ] In-app GPS recording, iOS half — inherits the Android build's wire shape and `source` convention once the iOS app itself exists (see the iOS Path 2 item above, which this depends on).

---

## Phase 3 — Finalized design + mobile browser support

The shipped UI so far is functional scaffolding, not a finished product — confirmed directly, not just by impression: hand-rolled inline SVG icons (`ActivitiesPanel.tsx`'s `EyeIcon`/`TrashIcon`/`PencilIcon`/`RowIcon`, not a considered icon system), the bare `system-ui` font stack with no custom typography (`index.css:3`), zero CSS custom properties/design tokens anywhere (every color/spacing value is a one-off literal scattered through the stylesheet), and almost no animation — 2 `transition:`/`animation:`/`@keyframes` occurrences in the whole ~2,400-line file. This phase is the pass that finishes it, across both desktop and mobile, ending in an explicit design freeze: "this is how it will look — no more changes."

- [ ] A real icon set — replace the hand-rolled inline SVG icons with a considered, consistent system (library or custom-drawn, but one coherent set, not ad-hoc per-component SVGs).
- [ ] Real typography — move off the bare `system-ui` stack to a deliberate font choice/pairing.
- [ ] An actual design system — color palette, spacing scale, and component styling expressed as CSS custom properties/tokens, not one-off hardcoded values.
- [ ] An animation/transition pass — micro-interactions (hover, focus, panel open/close, loading states) that are currently almost entirely absent.
- [ ] Mobile browser support, folded into this same pass rather than treated separately — CSS/layout work already exists (`index.css`'s `@media (max-width: 768px)` layer, `IMPLEMENTATION.md` §5.9, `SPEC.md` §15), but the actual experience has been reported directly as unusable, not just rough, and needs the same real-device testing and rework this phase's desktop work gets, not CSS review assumed to already be correct.
- [ ] Design freeze: once this pass lands, declare the visual design final and communicate it as such — the explicit milestone this phase produces, not an open-ended polish effort.

This pass is where HoldMyTrack's palette, typography and icon set come from for the product as a whole, not for the web alone. The Android app is built unstyled on purpose and carries its own phase for landing this output on the platform (`apps/android/docs/ROADMAP.md`, Phase 5) — it inherits the freeze rather than deciding a second visual design, since two clients that each invented their own would not read as one product.

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
- [ ] These connectors are the one part of the web's Import → Sync tab (`docs/IMPLEMENTATION.md` §4.0.1) that's actually triggerable from the tab itself — connect/disconnect, and (once token-refresh runs on a schedule) a last-synced/status summary alongside Health Connect/GPS Logger's own read-only rows there. Sync stayed read-only-only through Phase 2 specifically because neither on-device source can be triggered from the web; a cloud connector can.

### Explorer-tile gamification

- [ ] `user_tiles` table exists, nothing reads it yet — scoring queries (total tiles, max square, max connected cluster, per-region coverage %, `IMPLEMENTATION.md` §4.4) and a UI surface for them (a natural fit on the Profile page, alongside the activity grid).

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
- [ ] Working data export and deletion endpoints (account deletion doesn't exist yet — confirm and build if missing). **Naming collision to resolve when this is built:** the map screen's own "Export" header control (FR-4.10) already owns that word for a completely different action (a framed PNG capture) — whoever builds this should either rename FR-4.10's control (candidates raised: "Share," "Save") or pick a different label here, so the two don't collide.
- [ ] Health Connect data-type declarations in the Play Console, scoped to only what's actually used (Phase 2 builds the app; the declaration work belongs here).
- [ ] Confirm we don't need a cookie consent banner — as of this writing the web client sets exactly one cookie (`holdmytrack_session`: `HttpOnly`, `SameSite=Lax`, `Secure` under HTTPS, no `localStorage`/analytics/tracking anywhere in `apps/web`), which should fall under the ePrivacy Directive Art. 5(3) "strictly necessary" exemption — no consent required, only a plain-language disclosure in the privacy policy. Re-check this conclusion at launch time (cookie/analytics usage can drift) and again the day anything non-essential (analytics, an ad pixel, marketing tracking) is added, since that would flip the answer.

### Private locations (`privacy_zones` table, §3.7; FR-8.1)

- [x] Creation UI — on the map (`PrivateLocationsPanel.tsx`, opened from the account menu or Settings).
- [x] `GET`/`POST`/`PATCH`/`DELETE /v1/private-locations` endpoints.
- [x] Apply at ingest — the leading and trailing parts of a track inside a location (§4.1 step 3's `ClipEnds`); replaces the fixed endpoint trim (ADR-0010).
- [x] Retroactive `reprivacy` job (§7) — affected activities show Pending until reprocessed.
- [ ] Split a track that passes *through* a Private location mid-way: store `activities.trajectory` as a multi-part geometry (MultiLineString, or gap markers) so the inside part can be dropped without drawing a chord across the circle. Touches the tracks tile, the track editor, colored bands/profile (which assume one line), and Android's track rendering.

---

## Phase 7 — Social (deliberately not committed)

`VISION.md` §5.5/§5.6 is explicit that this should not be scheduled, let alone built, until the funding base can absorb the moderation and trust-and-safety staffing it requires — a fog map is a precise record of where someone lives, and a social graph on top of that is a threat-model change, not a feature. No steps are listed here on purpose; the first real step is revisiting §6's funding numbers, not writing code.

---

## Ongoing, not phase-bound

- [ ] Re-measure the funding-model assumptions (§4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold.