# Roadmap

Last updated: 2026-10-03.

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

**Shipped and deployable.** Every feature in `SPEC.md`'s FR-1 through FR-15 — auth and account management, the no-signup demo, activity upload/ingestion (file, `.zip`, Google Takeout), Normal/Fog of War/Heatmap map modes with pace-colored segments and high-res export, the Activities panel and its filters, track editing, Private locations, the date-range picker, the per-account activity graph, per-activity pace, distance trends, the public About and Help pages, the Donate link, the admin panel, English and Russian, and Stories on the web and in the Android app, and Spots on the web and in the Android app — is built and documented there; not re-enumerated here.

### Pre-launch validation — gates any public launch (Milestone 2), regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after. The app side is built (`IMPLEMENTATION.md` §4.16), and the `holdmytrack` collective applied to Open Source Collective as fiscal host on 2026-09-24; what's left: once approved, set the slug — `services/server/internal/web/web.go`'s `OpenCollectiveSlug` — and replace the About page template's "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free, funded by its users" as credible before building further.

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
- [x] In-app GPS recording, Android half — a convenience capture for casual, watch-free activities (a road trip, a dog walk), not a fitness-tracker replacement: GPS only, no sensor data, no training metrics (`VISION.md` §4.1, §1.1). Submits directly through the existing ingest pipeline once a recording stops, reusing the sync endpoint's payload shape rather than opening a new one ([ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)) — no new server-side path, just a new `source` value. `apps/android/docs/IMPLEMENTATION.md` §7 carries the detail; `docs/SPEC.md` FR-3.8 is the behavior spec.
- [x] Photos on Android — an activity's and an open Story's photos as markers on the route with a popup, and a Photos tab in the Edit window — added from the photo picker with the web's EXIF rules and resizing, a slider for a photo the server can't place and for moving one (`SPEC.md` FR-16, ADR-0024; `apps/android/docs/SPEC.md` FR-2.10).
- [x] The map's Activities panel on Android — the phone web's sheet ported with the same layout and rules: the list with its Type and Distance filters, selection on the list and the map, Show/Hide, Edit (fields and track) and Delete, the selected activity's pace bands, and the Sync and Privacy tabs; the separate sync screens folded into one Sync Source window (`apps/android/docs/SPEC.md` FR-2.7, FR-3.5, FR-4.1).

### iOS

- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry — the stronger of the two on-device paths, and the one that inherits the payload and sync-cursor design Android settles.
- [ ] In-app GPS recording, iOS half — inherits the Android build's wire shape and `source` convention once the iOS app itself exists (see the iOS Path 2 item above, which this depends on).

---

## Phase 4 — More sources

Connecting the app to third-party services, and reading the exports of ones it can't connect to.

### Path 3 — Google Maps Timeline import

For travellers whose only route history is Timeline (`VISION.md` §3.1, §5.5). Import only: no continuous location logging (`VISION.md` §1.1).

- [ ] Try the render on two or three real Timeline exports before building anything: Timeline is visits plus sparse, mode-guessed movement between them, not a recorded track, so drawn as-is the fog clears in straight lines through buildings and across lakes. Decide from what it looks like how Timeline-sourced data is drawn — for example, clearing fog only around visits, or keeping inferred movement out of the fog — and how flights and guessed modes show.
- [ ] Settle which formats to read: the phone's on-device export, and the older account-side `Semantic Location History` from past Takeouts, which differ; neither is documented, so the reader is built against real files.
- [ ] Upload with a date range and a preview of what will be imported, not a whole-history dump — it's the first import of data the user never chose to record as an activity. Private locations apply as for every other source (`VISION.md` §7).
- [ ] Measure the storage a multi-year Timeline adds per account against `VISION.md` §4.3's assumptions.

### Prerequisites — gate the specific connectors below, not this phase's other work

- [ ] Get Garmin's Connect Developer Program licence position in writing for a free, donation-funded service (`VISION.md` §4.1, §8.3) — the one item that can impose a fixed cost this funding model cannot absorb.
- [ ] File the Wahoo partner-API application (lead time, not cost, is the risk).
- [ ] File the COROS partner-API application.

### Path 1 — cloud connectors (gated on the prerequisites above)

- [ ] Generic OAuth connection scaffolding — `connections` table already exists (`migrations/0002_activities.sql`, `IMPLEMENTATION.md` §3.2); build the authorize/callback/token-refresh flow once, provider-agnostic, before any specific provider.
- [ ] Garmin connector (after the licence prerequisite is settled).
- [ ] Wahoo connector (after partner approval).
- [ ] COROS connector (after partner approval).
- [ ] Deauthorization deletion for each connector as it ships, not after — Garmin/Wahoo/COROS contractually require it (`VISION.md` §7).
- [ ] These connectors are the one part of the web's Sync page, `/sync` (`docs/IMPLEMENTATION.md` §4.0.1), that's actually triggerable from the page itself — connect/disconnect, and (once token-refresh runs on a schedule) a last-synced/status summary alongside Health Connect/GPS Logger's own read-only rows there. Sync stayed read-only-only through Phase 2 specifically because neither on-device source can be triggered from the web; a cloud connector can.

---

## Phase 5 — Cost control

An engineering requirement, can land alongside any of the above (`IMPLEMENTATION.md` §5.7).

- [ ] Retention/dormancy policy: tier `activity_streams` to cold storage or drop it after N months of inactivity (`users.last_seen_at`), keeping summaries and fog rasters so the map still renders; warn by email first; recoverable by re-upload. Activity photos (`photos/{userID}/`, ADR-0024) are the other per-user line that only grows; decide whether a dormant account keeps them, keeps the thumbnails only, or loses them after the warning.
- [ ] Raw payload expiry schedule (object storage) — note this caps how far back a `reprivacy` job (a Private location change) can reach; document the tradeoff wherever it's implemented.
- [ ] Per-user quotas (activity count, total points) — bounds one pathological account's cost, not a monetization lever.
- [ ] Rate limits on upload, export, and tile requests (the auth endpoints already have `fixedWindowLimiter` — reuse it), plus a CDN/object-store spend cap.
- [ ] Conditional reads for `GET /v1/spots/captures` — the web map reads the whole list again on every window focus and tab return (`SPEC.md` FR-15.2), so an `ETag` built from the account's capture count and latest `captured_at`, answered with `304 Not Modified` when it matches `If-None-Match`, keeps the repeat reads bodyless; the Android app's reads get the same for free. Preferred over a `?since=` cursor, which only sees new rows and misses a capture that cascades away with its spot.
- [ ] Cost-per-active-user measurement from day one — with monthly donations and the share of active users who give, the numbers Milestone 2's exit gate and the Milestone 3 decision are made from (`VISION.md` §5, §6).

---

## Phase 6 — Compliance

Non-negotiable, GDPR Art. 9 special-category data (`VISION.md` §7).

- [ ] DPIA before any public launch.
- [ ] EU-region hosting for EU users.
- [ ] Working data export and deletion endpoints (account deletion doesn't exist yet — confirm and build if missing). The Play Store needs deletion reachable both inside the app and from a web page (`apps/android/docs/ROADMAP.md` Phase 6). Deleting an account has to remove its objects as well as its rows — `raw/`, `fog/`, `heatmap/`, `activity-masks/` and `photos/{userID}/` (activity photos cascade in the database, their images don't) — and an export should include the photos. Until it exists, Help's `#delete-account` section asks people to email for deletion, and it is also the Meta app's data-deletion instructions URL (`IMPLEMENTATION.md` §4.22) — update both when this ships. **Naming collision to resolve when this is built:** the map screen's own "Export" header control (FR-4.10) already owns that word for a completely different action (a framed PNG capture) — whoever builds this should either rename FR-4.10's control (candidates raised: "Share," "Save") or pick a different label here, so the two don't collide.
- [ ] A privacy policy page — none exists yet. A public launch needs one under GDPR regardless, and the Play Store listing, its Data safety form and the Health Connect declaration all point to it (`apps/android/docs/ROADMAP.md` Phase 6). It has to name Grafana Labs as a processor of the logs and metrics (ADR-0030), and say that a deleted account stays in the backups for up to 8 weeks (ADR-0029).
- [ ] Health Connect data-type declarations in the Play Console, scoped to only what's actually used (Phase 2 builds the app; the declaration work belongs here).
- [ ] Confirm we don't need a cookie consent banner — as of this writing the web client sets two cookies (`holdmytrack_session`, and `hmt_lang` once someone picks a language in the header's menu, ADR-0025; both `HttpOnly`, `SameSite=Lax`, `Secure` under HTTPS) and one `localStorage` key (`hmt_theme`, the chosen theme), with no analytics or tracking anywhere in `apps/web`. The session cookie is strictly necessary and the other two are user-interface preferences set at the user's request, which should all fall under the ePrivacy Directive Art. 5(3) "strictly necessary" exemption — no consent required, only a plain-language disclosure in the privacy policy. Re-check this conclusion at launch time (cookie/analytics usage can drift) and again the day anything non-essential (analytics, an ad pixel, marketing tracking) is added, since that would flip the answer.

---

## Phase 7 — Social graph (Milestone 3, deliberately not committed)

Public pages, followers and the rest. `VISION.md` §5.7/§5.8 is explicit that this isn't scheduled, let alone built, until Milestone 2 has shown what community funding can carry — a fog map is a precise record of where someone lives, and a social graph on top of that brings moderation and trust-and-safety work that is staff cost, not server cost. No steps are listed here on purpose; the first real step is the Milestone 3 decision — paid features, community funding, sponsors, or not building it at all — made from Milestone 2's numbers, not writing code.

---

## Ongoing, not phase-bound

- [ ] Check the Russian translation — run the Android app in Russian on a real device, and have a native speaker review the Russian across the web, the server's pages and emails, and the app (`IMPLEMENTATION.md` §4.21, ADR-0014).
- [ ] Re-measure the funding-model assumptions (`VISION.md` §4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold — they feed Milestone 2's exit gate.