# Roadmap

Last updated: 2026-09-17.

## How to read this document

This is the master checklist from "what exists today" to "the full product `VISION.md` describes" — every remaining piece of work, broken into steps small enough to pick up and finish independently. It does not restate design detail already written down elsewhere:

- **`VISION.md`** is the authority on *why* and *in what order* (§5's phases, the funding model, the ingest-path strategy) — note that this file's delivery order (Mobile, then a design-finalization pass, before Analysis+Cloud sources) currently diverges from `VISION.md` §5's stated sequence; not yet reconciled.
- **`SPEC.md`** is the authority on *what's actually shipped*, feature by feature, with preconditions/inputs/outputs/error cases (§1.2 states scope precisely).
- **`IMPLEMENTATION.md`** is the authority on *how* each shipped piece works, and carries most of the unbuilt pieces' own design already worked out (schema, API shape, algorithm) — this document points at that design rather than re-deriving it.
- **`AGENTS.md`** is the fastest way to see what's built, mapped to actual files.

Checkboxes are the source of truth for progress; re-check them against the three docs above rather than trusting this file's memory of itself if it's been a while. A step that names a file or table already assumes the reader will open the referenced §/FR for the real detail.

---

## Phase 1 — MVP

**Shipped and deployable.** Every feature in `SPEC.md`'s FR-1 through FR-9 — auth and account management, the no-signup demo, activity upload/ingestion (file, `.zip`, Google Takeout), Normal/Fog of War/Heatmap map modes with colored zone segments and high-res export, the Activities panel and its filters, the date-range picker, the per-account activity graph, per-activity pace/heart-rate, and distance trends — is built and documented there; not re-enumerated here.

### Email verification + demo without real ingest — reverses the "no email verification" simplicity call `IMPLEMENTATION.md`:741 documents, now that real sync's cost makes that trade-off worth revisiting

Sync (Health Connect ingest today, cloud connectors in Phase 4) does real, non-trivial work per activity — classification, batching, server-side parsing, distance/duration calc, fog/heatmap tile mask rendering. An account whose email was mistyped at signup can never complete a password reset, so any of that work is permanently stranded on an account nobody can ever get back into. The demo account (`POST /v1/auth/demo`, `IMPLEMENTATION.md` §4.10) has the same problem in a sharper form: it requires no signup at all, is rate-limited but not identity-checked, and today is verified to allow a real authenticated upload before being purged a day later — compute anyone can trigger repeatedly for data guaranteed to be thrown away.

- [ ] Real account creation gates the map (and every authenticated route) behind email verification — hold on a "verify your email" screen until `email_verified = true`. Unaffected: the existing signed-out, unauthenticated basemap stays public exactly as today (`docs/ARCHITECTURE.md`'s "a signed-out FitMap is a working map rather than a login wall" still holds — this only gates a freshly created, unverified account, not anonymous browsing).
- [ ] The verify-email screen offers both resend and change-email — resend alone doesn't help someone who typed the address wrong in the first place, which is the actual case this change exists to catch.
- [ ] Demo accounts drop upload/sync entirely — no Health Connect permission flow, no `POST /v1/activities/upload`/`sync/activities` access — and are seeded instead from a small, curated, one-time-built library of preset activities (GPX/FIT-shaped, stored like any other activity) so Normal/Fog of War/Heatmap/Trends all have something to show immediately.
- [ ] Preset library selection is a one-time lookup, not per-request generation: build roughly 15-30 realistic routes once (a city park loop, a coastal run, a river path, etc.) spread across major regions, then pick the nearest to the demo account's coarse IP-based country/region at start time — reusing the remote-address extraction the demo rate limiter already does (`IMPLEMENTATION.md` §4.10's 429 on the 6th demo start/hour from one address) — falling back to one default global set when nothing nearby exists. No live route-generation engine: synthesizing plausible roads/pace/elevation per request would cost more than the sync work this change removes, undoing the point of it.

### Group edit type — extends FR-5.12/FR-5.13's header-toolbar bulk actions to Type, alongside the existing Group visible/Delete group

FR-5.10 already lets a person retype a single activity's Type (free text, with a `<datalist>` of the account's other existing types as a convenience — no controlled vocabulary, per `IMPLEMENTATION.md` §4.7.2). FR-5.12 (Group visible) and FR-5.13 (Delete group) already put bulk header-toolbar actions over the checked group (FR-5.6) next to their own per-row icons. A "Group edit" button follows the same shape for Type: select one or more rows, open a dialog offering only the Type field (same input/datalist as FR-5.10's), submit once, and every checked activity gets that Type.

- [ ] Header toolbar gets a third icon-only bulk action (pencil, matching FR-5.10's per-row icon) above the per-row edit column, enabled whenever the checked group (FR-5.6) is non-empty — same enablement rule FR-5.12/FR-5.13 already use.
- [ ] The dialog reuses FR-5.10's Type input/datalist exactly, but shows only Type (no Name/Description — those are inherently per-activity and don't make sense set identically across a group), and states the count of activities it will affect.
- [ ] **No backend change.** `PATCH /v1/activities/{id}` stays single-item; the client loops over the checked ids, calling it once per id — the same pattern FR-5.13's Delete group already established (sequential per-id calls to the existing single-item endpoint, one combined refresh after), not a new batch endpoint. A batch PATCH was considered and rejected: it would need to invent request/error/atomicity semantics (partial failure handling, transaction-or-not) that the actual need — apply one shared Type to a handful of hand-picked rows — never requires, and it would leave Edit group and Delete group with inconsistent shapes for no functional gain at this scale.
- [ ] Each per-id call must resend that activity's own existing `name`/`description` alongside the new shared `activityType` — `updateActivity` is full-replace, not partial (`api.ts`), so naively sending only the new Type to every id would blank out every checked activity's name and description. The panel already holds full `Activity` objects for the checked group (the same data `checkedActivities` already provides Delete group's confirm message), so this is just building the right per-row payload, not a new data dependency.
- [ ] Once it lands: FR-5.2's TYPE filter chips, each row's TYPE label, and FR-5.10's own `<datalist>` all recompute for free from the refreshed data (`activityFacets.ts` is already purely derived from loaded activities) — no changes needed there, same as FR-5.10 required none.

### Export attribution + FitMap logo — closes a compliance gap in FR-4.10, and adds a small brand mark

`style.ts:28` already documents that OSM/Protomaps attribution is mandatory, not decorative — the basemap is an ODbL "Produced Work," and the comment states credit "has to be visible on the map and on any export." But `exportMap.ts:68` sets `attributionControl: false` on the offscreen export map instance, and nothing else draws attribution onto the canvas before `toBlob()` — so FR-4.10's exported PNGs carry no attribution at all today, contradicting the code's own stated requirement. This is a real compliance bug in already-shipped functionality, not a cosmetic gap.

- [ ] Draw `style.ts`'s own `ATTRIBUTION` text onto the export canvas before `toBlob()` in `exportMap.ts` — the fix for the gap above. Not optional or togglable: it's a license requirement, not a preference.
- [ ] Add a small FitMap logo/wordmark in a non-competing corner of the export (sharing the same lower-corner strip as the attribution text, small and low-contrast, never covering map content) — free brand exposure on exports that get shared, which fits `VISION.md` §6's free-forever, donation-funded, no-ad-budget model. Several free, sharing-driven apps (Strava, Peloton, Duolingo) put the same kind of subtle mark on their own shareable images for the same reason.
- [ ] The logo is on by default, with a simple toggle to turn it off per export (or a persisted preference) — a courtesy, not a paywall gate, since there is no paid tier here to protect. Unlike the attribution text, this one is genuinely optional.
- [ ] Both are drawn in `exportMap.ts` right before the canvas's `toBlob()` call, in the same pass — the natural point, since both need to be baked into the raster itself, not just shown via the live map's DOM-based `AttributionControl`, which the export path bypasses entirely.

### Production deployment — `freefitmap.com` is live as a sandbox, not yet Production

**`https://freefitmap.com` is up and serving the real stack** (confirmed 2026-09-17: `200` on the homepage, a correct `401` from the authenticated `/v1/auth/me`) — but deliberately not called "Production" yet. It's a 1 CPU / 4 GB VPS, too small to even build the Docker images on-box (`compose.prod.yml`'s `build:` stages need more than that box has — images have to be built elsewhere and shipped in, not built in place the way `docs/DEPLOY.md` currently assumes), it has zero traffic, and it has never been announced anywhere. Right now it's a sandbox for the operator's own personal use (their own account, their own synced history) — real dogfooding, not a public launch, so Phase 6's compliance gates don't bind yet since no other person's data is on it. It becomes "Production" once the remaining items below are actually true of it.

- [x] `compose.prod.yml` + `apps/web/Dockerfile`'s `build`/`serve` stages + `apps/web/docker/Caddyfile` — a minimal single-VPS topology (Postgres+PostGIS, `api`, `worker`, Caddy), verified to build and validate.
- [x] TLS/HTTPS readiness — the session cookie's `Secure` flag is derived from `APP_BASE_URL`; now actually exercised live at `https://freefitmap.com`.
- [x] A VPS and domain are provisioned and reachable (`freefitmap.com`) — the box itself is real, even though it's sandbox-sized, not production-sized.
- [ ] Confirm R2 (or equivalent object storage) is actually wired in, not just Postgres — `docs/DEPLOY.md` end to end hasn't been confirmed run against this box specifically.
- [ ] Resolve the on-box build constraint: either build images off-box (locally or in CI) and ship them to the VPS, or move to a bigger box — `docs/DEPLOY.md` needs updating either way, since its current assumption (build in place) doesn't hold on this hardware.
- [ ] Size up from sandbox hardware once real traffic is expected — 1 CPU / 4 GB is fine for a single dogfooding account, not sized for this being called Production.
- [ ] CDN in front of the basemap `.pmtiles` archive and tile endpoints (§4.3 of the business plan — this is the one basemap cost that scales with usage without one). `config.ts`'s `basemapOrigin()`/`VITE_BASEMAP_ORIGIN` make this configurable now; nothing is actually hosted on a CDN yet — `docs/DEPLOY.md`'s default is a bind-mounted file on the VPS itself, not a CDN.
- [ ] Backups (Postgres, object storage) and a restore drill — **the most urgent of these gaps now that real personal data (synced Health Connect history) is starting to land on this box**, not just disposable dev fixtures.
- [ ] Basic monitoring/alerting (error rate, queue depth, cost-per-user from Phase 5's measurement item).

### Pre-launch validation — gates any public launch, regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free forever, funded by users" as credible before building further.

---

## Phase 2 — Mobile

Native apps whose core job is exporting device-recorded health data to FitMap (Path 2 on-device sync, `docs/adr/0001-three-independent-ingest-paths.md`) — reading the platform's own health store rather than a cloud API. Needed no licensing gate the way Phase 4's cloud connectors do: HealthKit/Health Connect access itself isn't in question, only how much of it (see the two confirmation items below, which are this phase's own first steps, not an external blocker).

- [ ] Confirm the Samsung Health Connect route-geometry limitation empirically, not just from documentation (`VISION.md` §4.1) — Samsung's own developer docs already state `EXERCISE_ROUTE` cannot be read via Health Connect; this is double-checking in case reality is better than documented, not an open question blocking the build.
- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [ ] Android app: Health Connect sync, foreground-only (`READ_EXERCISE_ROUTES` can't be requested programmatically, and background route reads return `ConsentRequired` even with "Always allow" granted — a platform constraint, not an implementation shortcut). Samsung Galaxy Watch is unsupported — it never exposes route geometry, and FitMap only ingests activities that have one. **Build this one first**, so the Path 2 sync contract is designed against the harder platform's constraints. `apps/android/docs/ROADMAP.md` carries this app's own phases and its server-side prerequisites.
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry — the stronger of the two on-device paths, and the one that inherits the payload and sync-cursor design Android settles.
- [ ] In-app GPS recording (Android, then iOS) — a convenience capture for casual, watch-free activities (a road trip, a dog walk), not a fitness-tracker replacement: GPS only, no sensor data, no training metrics (`VISION.md` §4.1, §1.1). Submits directly through the existing ingest pipeline once a recording stops, reusing the sync endpoint's payload shape rather than opening a new one ([ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)) — no new server-side path. `apps/android/docs/ROADMAP.md` carries the phase plan.

### Cross-source deduplication (`IMPLEMENTATION.md` §4.6) — unavoidable once a second ingest source exists, which mobile sync is, ahead of Phase 4's cloud connectors under this order

- [x] Fuzzy identity at ingest: same user, same activity type, start within half a minute either way, distance within ~1% — applied as a window around the incoming activity rather than equality on a pre-rounded bucket, so a pair a few seconds apart matches every time instead of only when it happens not to straddle a boundary (`IMPLEMENTATION.md` §4.6).
- [x] Superseded-record handling: prefer the richest record (geometry over none, more stream channels over fewer); mark others `superseded_by`, don't delete, so a user can see why an activity disappeared (`GET /v1/activities/duplicates`). Deleting the winner re-ranks the copies it releases rather than making them all live at once.
- [ ] Surface superseded activities somewhere in the Activities panel or Profile, so "disappeared" activities are discoverable, not silently gone.

---

## Phase 3 — Finalized design + mobile browser support

The shipped UI so far is functional scaffolding, not a finished product — confirmed directly, not just by impression: hand-rolled inline SVG icons (`ActivitiesPanel.tsx`'s `EyeIcon`/`TrashIcon`/`PencilIcon`/`RowIcon`, not a considered icon system), the bare `system-ui` font stack with no custom typography (`index.css:3`), zero CSS custom properties/design tokens anywhere (every color/spacing value is a one-off literal scattered through the stylesheet), and almost no animation — 2 `transition:`/`animation:`/`@keyframes` occurrences in the whole ~2,400-line file. This phase is the pass that finishes it, across both desktop and mobile, ending in an explicit design freeze: "this is how it will look — no more changes."

- [ ] A real icon set — replace the hand-rolled inline SVG icons with a considered, consistent system (library or custom-drawn, but one coherent set, not ad-hoc per-component SVGs).
- [ ] Real typography — move off the bare `system-ui` stack to a deliberate font choice/pairing.
- [ ] An actual design system — color palette, spacing scale, and component styling expressed as CSS custom properties/tokens, not one-off hardcoded values.
- [ ] An animation/transition pass — micro-interactions (hover, focus, panel open/close, loading states) that are currently almost entirely absent.
- [ ] Mobile browser support, folded into this same pass rather than treated separately — CSS/layout work already exists (`index.css`'s `@media (max-width: 768px)` layer, `IMPLEMENTATION.md` §5.9, `SPEC.md` §13), but the actual experience has been reported directly as unusable, not just rough, and needs the same real-device testing and rework this phase's desktop work gets, not CSS review assumed to already be correct.
- [ ] Design freeze: once this pass lands, declare the visual design final and communicate it as such — the explicit milestone this phase produces, not an open-ended polish effort.

This pass is where FitMap's palette, typography and icon set come from for the product as a whole, not for the web alone. The Android app is built unstyled on purpose and carries its own phase for landing this output on the platform (`apps/android/docs/ROADMAP.md`, Phase 5) — it inherits the freeze rather than deciding a second visual design, since two clients that each invented their own would not read as one product.

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

### Explorer-tile gamification

- [ ] `user_tiles` table exists, nothing reads it yet — scoring queries (total tiles, max square, max connected cluster, per-region coverage %, `IMPLEMENTATION.md` §4.4) and a UI surface for them (a natural fit on the Profile page, alongside the activity grid).

---

## Phase 5 — Cost control

An engineering requirement, can land alongside any of the above (`IMPLEMENTATION.md` §5.7).

- [ ] Retention/dormancy policy: tier `activity_streams` to cold storage or drop it after N months of inactivity (`users.last_seen_at`), keeping summaries and fog rasters so the map still renders; warn by email first; recoverable by re-upload.
- [ ] Raw payload expiry schedule (object storage) — note this caps how far back a future `reprivacy` job or re-trim can reach; document the tradeoff wherever it's implemented.
- [ ] Per-user quotas (activity count, total points) — bounds one pathological account's cost, not a monetization lever.
- [ ] Rate limits on upload, export, and tile requests (the auth endpoints already have `fixedWindowLimiter` — reuse it), plus a CDN/object-store spend cap.
- [ ] Cost-per-active-user measurement from day one — the number that decides whether `VISION.md` §6's funding model actually works.

---

## Phase 6 — Compliance

Non-negotiable, GDPR Art. 9 special-category data (`VISION.md` §7).

- [ ] DPIA before any public launch.
- [ ] EU-region hosting for EU users.
- [ ] Working data export and deletion endpoints (account deletion doesn't exist yet — confirm and build if missing).
- [ ] Health Connect data-type declarations in the Play Console, scoped to only what's actually used (Phase 2 builds the app; the declaration work belongs here).
- [ ] Confirm we don't need a cookie consent banner — as of this writing the web client sets exactly one cookie (`fitmap_session`: `HttpOnly`, `SameSite=Lax`, `Secure` under HTTPS, no `localStorage`/analytics/tracking anywhere in `apps/web`), which should fall under the ePrivacy Directive Art. 5(3) "strictly necessary" exemption — no consent required, only a plain-language disclosure in the privacy policy. Re-check this conclusion at launch time (cookie/analytics usage can drift) and again the day anything non-essential (analytics, an ad pixel, marketing tracking) is added, since that would flip the answer.

### Privacy zones (schema exists, nothing else does — `privacy_zones` table, §3.7)

- [ ] Creation UI (likely on the map, or from SettingsPage.tsx alongside Privacy Trim).
- [ ] `POST`/`DELETE` endpoints for a zone.
- [ ] Apply at ingest (§4.1 step 3 already has the placeholder logic for this — wire a real zone set in).
- [ ] Retroactive `reprivacy` job (`jobs.kind = 'reprivacy'`, §7): re-parses each affected activity's raw payload and re-runs ingest steps 2–6 against the new zone set — the same code path as initial ingest, not a bespoke re-clip. Also mark affected fog/heatmap tiles dirty.

---

## Phase 7 — Social (deliberately not committed)

`VISION.md` §5.5/§5.6 is explicit that this should not be scheduled, let alone built, until the funding base can absorb the moderation and trust-and-safety staffing it requires — a fog map is a precise record of where someone lives, and a social graph on top of that is a threat-model change, not a feature. No steps are listed here on purpose; the first real step is revisiting §6's funding numbers, not writing code.

---

## Ongoing, not phase-bound

- [ ] Keep `SPEC.md`/`IMPLEMENTATION.md`/`AGENTS.md` in sync with each change, per the cross-reference discipline already established (a change to one almost always means a small edit to the other two).
- [ ] Re-measure the funding-model assumptions (§4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold.