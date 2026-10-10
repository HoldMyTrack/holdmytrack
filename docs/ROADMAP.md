# Roadmap

Last updated: 2026-10-09.

## How to read this document

This is the master checklist from "what exists today" to "the full product `VISION.md` describes" — every remaining piece of work, broken into steps small enough to pick up and finish independently. It does not restate design detail already written down elsewhere:

- **`VISION.md`** is the authority on *why* and *in what order* (§5's milestones and phases, the funding model, the ingest-path strategy); its phase numbers match this file's.
- **`SPEC.md`** is the authority on *what's actually shipped*, feature by feature, with preconditions/inputs/outputs/error cases (§1.2 states scope precisely).
- **`IMPLEMENTATION.md`** is the authority on *how* each shipped piece works, and carries most of the unbuilt pieces' own design already worked out (schema, API shape, algorithm) — this document points at that design rather than re-deriving it.
- **`AGENTS.md`** is repository orientation — which document to read for what, not a status narrative of its own; `SPEC.md`/`IMPLEMENTATION.md` are where "what's built, mapped to actual files" actually lives.
- **`KNOWN_ISSUES.md`** is the opposite direction from this file: currently-open defects in already-shipped functionality, not planned work. A bug found while building something on this list belongs there, not here.

Checkboxes are the source of truth for progress; re-check them against the three docs above rather than trusting this file's memory of itself if it's been a while. A step that names a file or table already assumes the reader will open the referenced §/FR for the real detail. Once every checkbox in a subsection is checked, delete the subsection rather than leave it as a completed record — its design and rationale belong in `SPEC.md`/`IMPLEMENTATION.md` by the time it ships, not here. A partially-done section stays as-is until its own last checkbox is checked.

## Milestones

The phases below sit inside three milestones (`VISION.md` §5, ADR-0031):

1. **MVP** — Phases 1–3. Where HoldMyTrack is now; what's left of it is below.
2. **Release and community** — Phases 4–6. Its entry gate is Phase 1's pre-launch validation and Phase 6's DPIA and privacy policy, which any public launch needs. Its exit gate is a measured answer to what community funding can carry: Phase 5's cost per active user, set against monthly donations and the share of active users who give, over several months.
3. **Social graph** — Phase 7. Decided from Milestone 2's numbers, including whether to start it at all.

---

## Phase 1 — Upload + Map

**Shipped and deployable** — `SPEC.md` FR-1 through FR-16 describe it. What's left before the public launch:

### Merge a split activity back — server built, no control yet

Splitting an activity is built on the web (`SPEC.md` FR-5.17), and so is merging its parts back on the server (`POST /v1/activities/track-merge`, `IMPLEMENTATION.md` §4.7.8). Neither client offers Merge yet, so a split can't be undone from the app.

- [ ] Decide where Merge lives — a toolbar action over checked neighboring parts, or a "Merge with the next part" in the Edit window — and build it on the web, from the list rows' `split` field.
- [ ] Carry it to the Android app, next to its Split tool (`apps/android/docs/SPEC.md` FR-2.7 item 12).

### Pre-launch validation — gates any public launch (Milestone 2), regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after. The app side is built (`IMPLEMENTATION.md` §4.16), and the `holdmytrack` collective applied to Open Source Collective as fiscal host on 2026-09-24; what's left: once approved, set the slug — `services/server/internal/web/web.go`'s `OpenCollectiveSlug` — and replace the About page template's "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free, funded by its users" as credible before the public launch.

### Sign in with Facebook — built, not live

Sign in with Facebook is built (`SPEC.md` FR-1.10) and the Meta app exists, but Meta won't publish an app until its business portfolio passes Business Verification (`docs/DEPLOY.md` §4). Until then `holdmytrack.com` runs with `FACEBOOK_APP_ID` empty, so it shows no Facebook button. This isn't a launch gate: Google and email sign-in cover everyone.

- [ ] Get a business document for the "Holdmytrack" business portfolio. Meta accepts one of: an IRS 147C letter (EIN confirmation), a business bank statement, a business tax document, or a "Doing Business As" (DBA) filing. The name on it must match the portfolio's. A sole-proprietor EIN with "HoldMyTrack" as its trade name, or a county/state DBA filing, are the cheapest routes; so may be whatever legal standing the Open Collective fiscal host above gives the project. Check the legal and tax implications before filing just for this.
- [ ] Complete Business Verification with it, connect the Meta app to the verified portfolio and publish it (`docs/DEPLOY.md` §4 step 6), then set `FACEBOOK_APP_ID`/`FACEBOOK_APP_SECRET` in the server's `.env.prod` and recreate `api`.

---

## Phase 2 — Mobile

Path 2 on-device sync and in-app GPS recording (`VISION.md` §5.3). Android and iOS are tracked separately below; iOS reuses the sync contract Android settled: the payload shape, and the list of what's on the phone that the user ticks from, with the server answering what the account already has (`SPEC.md` FR-3.6).

### Android

- [ ] Confirm the Samsung Health Connect route-geometry limitation empirically, not just from documentation (`VISION.md` §4.1) — Samsung's own developer docs already state `EXERCISE_ROUTE` cannot be read via Health Connect; this is double-checking in case reality is better than documented, not an open question blocking the build.
- [x] Android app: Health Connect sync, foreground-only, end to end (`SPEC.md` FR-3.6). The app's own remaining work is in `apps/android/docs/ROADMAP.md`.
- [x] In-app GPS recording, Android half (`SPEC.md` FR-3.8, ADR-0007).
- [x] Photos on Android (`SPEC.md` FR-16, ADR-0024; `apps/android/docs/SPEC.md` FR-2.10).
- [x] The map's Activities panel on Android (`apps/android/docs/SPEC.md` FR-2.7, FR-3.5, FR-4.1).

### iOS

- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry, on the payload and the pick-what-to-send design Android settled.
- [ ] In-app GPS recording, iOS half — inherits the Android build's wire shape and `source` convention once the iOS app itself exists (see the iOS Path 2 item above, which this depends on).

---

## Phase 4 — More sources

Connecting the app to third-party services.

### Prerequisites — gate the specific connectors below

- [ ] Get Garmin's Connect Developer Program licence position in writing for a free, donation-funded service (`VISION.md` §4.1, §4.3, §8.3).
- [ ] File the Wahoo partner-API application (lead time, not cost, is the risk).
- [ ] File the COROS partner-API application.

### Path 1 — cloud connectors (gated on the prerequisites above)

- [ ] Generic OAuth connection scaffolding — `connections` table already exists (`migrations/0002_activities.sql`, `IMPLEMENTATION.md` §3.2); build the authorize/callback/token-refresh flow once, provider-agnostic, before any specific provider, with a picker: a connector lists what it would bring in and imports only what the user ticks (ADR-0039). That is how bulk history arrives, since archives aren't imported.
- [ ] Garmin connector (after the licence prerequisite is settled).
- [ ] Wahoo connector (after partner approval).
- [ ] COROS connector (after partner approval).
- [ ] Deauthorization deletion for each connector as it ships, not after — Garmin/Wahoo/COROS contractually require it (`VISION.md` §7).
- [ ] Connect/disconnect for each connector on the web's Sync page, `/sync` (`docs/IMPLEMENTATION.md` §4.0.1), and (once token-refresh runs on a schedule) a last-synced/status summary beside Health Connect/GPS Logger's read-only rows.

---

## Phase 5 — Cost control

An engineering requirement, can land alongside any of the above (`IMPLEMENTATION.md` §5.7).

- [ ] Retention/dormancy policy: tier `activity_streams` to cold storage or drop it after N months of inactivity (`users.last_seen_at`), keeping summaries and fog rasters so the map still renders; warn by email first; recoverable by re-upload. Activity photos (`photos/{userID}/`, ADR-0024) are the other per-user line that only grows; decide whether a dormant account keeps them, keeps the thumbnails only, or loses them after the warning.
- [ ] Raw payload expiry schedule (object storage) — note this caps how far back a `reprivacy` job (a Private location change) can reach; document the tradeoff wherever it's implemented.
- [ ] Per-user quotas (activity count, total points) — bounds one pathological account's cost.
- [ ] Rate limits on upload, export, and tile requests (the auth endpoints already have `fixedWindowLimiter` — reuse it), plus a CDN/object-store spend cap.
- [ ] Conditional reads for `GET /v1/spots/captures` — the web map reads the whole list again on every window focus and tab return (`SPEC.md` FR-15.2), so an `ETag` built from the account's capture count and latest `captured_at`, answered with `304 Not Modified` when it matches `If-None-Match`, keeps the repeat reads bodyless; the Android app's reads get the same for free. Preferred over a `?since=` cursor, which only sees new rows and misses a capture that cascades away with its spot.
- [ ] Cost-per-active-user measurement from day one — with monthly donations and the share of active users who give, the numbers Milestone 2's exit gate and the Milestone 3 decision are made from (`VISION.md` §5, §6).

---

## Phase 6 — Compliance

Gates any public launch: a precise location history is sensitive personal data under GDPR (`VISION.md` §7).

- [ ] DPIA before any public launch. As part of it, re-check that the cookies and browser storage `apps/web` and the server's pages set still match `/privacy`'s Cookies list (`SPEC.md` FR-10.6), all strictly necessary or a preference set at the user's request, so the site still needs no cookie banner; anything non-essential (analytics, an ad pixel, marketing tracking) would flip that.
- [ ] EU-region hosting for EU users.
- [x] A privacy policy page — `/privacy` (`SPEC.md` FR-10.6). It names the email delivery provider only generically; name it there once settled, and update the policy when EU-region hosting moves the data.
- [x] Account deletion and data export — Settings' Delete account and Download your data, on the web and in the Android app (`SPEC.md` FR-1.11, FR-1.12). Named "Download your data" so it doesn't collide with the map's Export control (FR-4.10).
- [ ] Health Connect data-type declarations in the Play Console, scoped to only what's actually used (Phase 2 builds the app; the declaration work belongs here).

---

## Phase 7 — Social graph (Milestone 3, deliberately not committed)

Public pages, followers and the rest (`VISION.md` §5.7, §5.8). No steps are listed on purpose: the first step is the Milestone 3 decision, made from Milestone 2's numbers, and not building it is one of its outcomes.

---

## Ongoing, not phase-bound

- [ ] Check the Russian translation — run the Android app in Russian on a real device, and have a native speaker review the Russian across the web, the server's pages and emails, and the app (`IMPLEMENTATION.md` §4.21, ADR-0014).
- [ ] Render a tile's own round trips side by side: `renderPyramidLevel` fetches its eight child PNGs one after another, and every tile stores its Fog and Heatmap PNGs one after another (`IMPLEMENTATION.md` §4.2 "Invalidation"). With masks in Postgres these are most of a render, about 10 round trips a pyramid tile; a 1,000-activity account still took 6–7 minutes at 80 ms a round trip (`PERFORMANCE.md`, 2026-10-10 dev).
- [ ] Once production has no `activity_tile_masks` row with a `mask_object_key` (`DEPLOY.md` §6), drop the column in a migration, with `renderAndStoreTile`'s object-store fallback, `RemoveActivityMasks`' object removal and the `activity-masks/{id}/` prefix removals on delete, edit and purge (ADR-0040).
- [ ] Re-measure the funding-model assumptions (`VISION.md` §4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold — they feed Milestone 2's exit gate.