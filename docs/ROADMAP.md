# Roadmap

Last updated: 2026-10-13.

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

### Pre-launch validation — gates any public launch (Milestone 2), regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after. The app side is built (`IMPLEMENTATION.md` §4.16), and the `holdmytrack` collective applied to Open Source Collective as fiscal host on 2026-09-24; what's left: once approved, set the slug — `services/server/internal/web/web.go`'s `OpenCollectiveSlug` — and replace the About page template's "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free, funded by its users" as credible before the public launch.

### Sign in with Facebook — built, not live

Sign in with Facebook is built (`SPEC.md` FR-1.10) and the Meta app exists, but Meta won't publish an app until its business portfolio passes Business Verification (`docs/DEPLOY.md` §4). Until then `holdmytrack.com` runs with `FACEBOOK_APP_ID` empty, so it shows no Facebook button. This isn't a launch gate: Google and email sign-in cover everyone.

- [ ] Get a business document for the "Holdmytrack" business portfolio. Meta accepts one of: an IRS 147C letter (EIN confirmation), a business bank statement, a business tax document, or a "Doing Business As" (DBA) filing. The name on it must match the portfolio's. A sole-proprietor EIN with "HoldMyTrack" as its trade name, or a county/state DBA filing, are the cheapest routes; so may be whatever legal standing the Open Collective fiscal host above gives the project. Check the legal and tax implications before filing just for this.
- [ ] Complete Business Verification with it, connect the Meta app to the verified portfolio and publish it (`docs/DEPLOY.md` §4 step 6), then set `FACEBOOK_APP_ID`/`FACEBOOK_APP_SECRET` in the server's `.env.prod` and recreate `api`.

---

## Phase 4 — More sources

Connecting the app to third-party services.

### Prerequisites — gate the specific connectors below

- [ ] File the Wahoo partner-API application (lead time, not cost, is the risk).
- [ ] File the COROS partner-API application.

### Path 1 — cloud connectors (gated on the prerequisites above)

- [ ] Generic OAuth connection scaffolding — `connections` table already exists (`migrations/0002_activities.sql`, `IMPLEMENTATION.md` §3.2); build the authorize/callback/token-refresh flow once, provider-agnostic, before any specific provider, with a picker: a connector lists what it would bring in and imports only what the user ticks (ADR-0039). That is how bulk history arrives, since archives aren't imported.
- [ ] Wahoo connector (after partner approval).
- [ ] COROS connector (after partner approval).
- [ ] Deauthorization deletion for each connector as it ships, not after — Wahoo and COROS contractually require it (`VISION.md` §7).
- [ ] Connect/disconnect for each connector on the web's Sync page, `/sync` (`docs/IMPLEMENTATION.md` §4.0.1), and (once token-refresh runs on a schedule) a last-synced/status summary beside Health Connect/GPS Logger's read-only rows.

---

## Phase 5 — Cost control

An engineering requirement, can land alongside any of the above (`IMPLEMENTATION.md` §5.7).

- [ ] Per-user quotas (activity count, total points) — bounds one pathological account's cost.
- [ ] Rate limits on upload, export, and tile requests (the auth endpoints already have `fixedWindowLimiter` — reuse it), plus a CDN/object-store spend cap.
- [ ] Conditional reads for `GET /v1/spots/captures` — the web map reads the whole list again on every window focus and tab return (`SPEC.md` FR-15.2), so an `ETag` built from the account's capture count and latest `captured_at`, answered with `304 Not Modified` when it matches `If-None-Match`, keeps the repeat reads bodyless; the Android app's reads get the same for free. Preferred over a `?since=` cursor, which only sees new rows and misses a capture that cascades away with its spot.
- [ ] Cost-per-active-user measurement from day one — with monthly donations and the share of active users who give, the numbers Milestone 2's exit gate and the Milestone 3 decision are made from (`VISION.md` §5, §6). An active user is one whose `users.last_seen_at` falls in the period (`IMPLEMENTATION.md` §5.7).

---

## Phase 6 — Compliance

Gates any public launch: a precise location history is sensitive personal data under GDPR (`VISION.md` §7).

- [ ] DPIA before any public launch — drafted in `docs/DPIA.md`; what's left is its section 7 pre-launch actions and the controller's sign-off.
- [ ] EU-region hosting for EU users.
- [x] A privacy policy page — `/privacy` (`SPEC.md` FR-10.6). It names the email delivery provider only generically; name it there once settled, and update the policy when EU-region hosting moves the data.
- [x] Account deletion and data export — Settings' Delete account and Download your data, on the web and in the Android app (`SPEC.md` FR-1.11, FR-1.12). Named "Download your data" so it doesn't collide with the map's Export control (FR-4.10).
- [x] Health Connect data-type declarations in the Play Console, scoped to only what's actually used — filed 2026-10-07 (`apps/android/docs/ROADMAP.md` Phase 6).

---

## Phase 7 — Social graph (Milestone 3, deliberately not committed)

Public pages, followers and the rest (`VISION.md` §5.7, §5.8). No steps are listed on purpose: the first step is the Milestone 3 decision, made from Milestone 2's numbers, and not building it is one of its outcomes.

---

## Ongoing, not phase-bound

- [ ] Describe the rest of the `/v1` JSON endpoints in `services/server/api/openapi.yaml` (`IMPLEMENTATION.md` §4.31), a few at a time: any test that already calls one checks it from then on.
- [ ] Decide one error shape for the JSON API — plain text from `http.Error`, or `{"error","message"}` everywhere — and describe it in the contract.
- [ ] Generate the web client's types from the contract (`openapi-typescript`) in place of `apps/web/src/api.ts`'s hand-written `*Body` interfaces.
- [ ] Move the Android app's `net/HoldMyTrackApi.kt` from hand-parsed `org.json` to typed data classes, checked against the contract.
- [ ] Check the Russian translation — run the Android app in Russian on a real device, and have a native speaker review the Russian across the web, the server's pages and emails, and the app (`IMPLEMENTATION.md` §4.21).
- [ ] Split `apps/web/src/map/MapView.tsx` by feature when the next feature touches it, the part that feature needs first: its state rules — Story mode, the Pending poll, track editing, selection and focus — into a reducer or a few custom hooks, under `verify:map` and the unit tests.
- [ ] Split the Android app's `MapFragment.kt` by feature when the next feature touches it, the part that feature needs first: the session and first-run check, Story mode, View on map, the notice panel, camera framing, photo markers — pure logic out first, with unit tests as `panel/PanelState` has, the UI wiring last.
- [ ] Review how long original uploads are kept, soon after the public launch, once real accounts show what they cost: today they're kept until the activity is deleted. They're the most sensitive thing held (`DPIA.md` R2) and likely the largest per-activity storage line, and an expired one can no longer be re-clipped after a Private location change, rebuilt by a track edit, or exported as the original (`IMPLEMENTATION.md` §5.7).
- [ ] Re-measure the funding-model assumptions (`VISION.md` §4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold — they feed Milestone 2's exit gate.
- [ ] The iOS app (`VISION.md` §5.3), likely after the public launch, with no date set: first confirm `HKWorkoutRoute` access with a throwaway app (`VISION.md` §4.1); then HealthKit sync, with `HKWorkoutRoute` for full GPS geometry, on the payload and the pick-what-to-send design Android settled (`SPEC.md` FR-3.6), and the site's own pages in a `WKWebView` for Settings, Profile, the Sync page and About (ADR-0042); then in-app GPS recording, on the Android build's wire shape and `source` convention (`SPEC.md` FR-3.8, ADR-0007).