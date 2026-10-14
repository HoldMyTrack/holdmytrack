# Roadmap

Last updated: 2026-10-14.

## How to read this document

This is the master checklist from "what exists today" to "the full product `VISION.md` describes" — every remaining piece of work, broken into steps small enough to pick up and finish independently. It does not restate design detail already written down elsewhere:

- **`VISION.md`** is the authority on *why* and *in what order* (§5's milestones, the funding model, the ingest-path strategy); this file follows its milestones.
- **`SPEC.md`** is the authority on *what's actually shipped*, feature by feature, with preconditions/inputs/outputs/error cases (§1.2 states scope precisely).
- **`IMPLEMENTATION.md`** is the authority on *how* each shipped piece works, and carries most of the unbuilt pieces' own design already worked out (schema, API shape, algorithm) — this document points at that design rather than re-deriving it.
- **`AGENTS.md`** is repository orientation — which document to read for what, not a status narrative of its own; `SPEC.md`/`IMPLEMENTATION.md` are where "what's built, mapped to actual files" actually lives.
- **`KNOWN_ISSUES.md`** is the opposite direction from this file: currently-open defects in already-shipped functionality, not planned work. A bug found while building something on this list belongs there, not here.

Checkboxes are the source of truth for progress; re-check them against the three docs above rather than trusting this file's memory of itself if it's been a while. A step that names a file or table already assumes the reader will open the referenced §/FR for the real detail. Once every checkbox in a subsection is checked, delete the subsection rather than leave it as a completed record — its design and rationale belong in `SPEC.md`/`IMPLEMENTATION.md` by the time it ships, not here. A partially-done section stays as-is until its own last checkbox is checked.

## Milestone 1 — MVP

Where HoldMyTrack is now (`VISION.md` §5, ADR-0031). The product is shipped and deployable — `SPEC.md` FR-1 through FR-17 describe it — and what's left is what the public launch needs.

### Before launch

Gates any public launch, regardless of which paths are live: validation that "free, funded by its users" reads as credible (`VISION.md` §5.1), and compliance, since a precise location history is sensitive personal data under GDPR (`VISION.md` §7).

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after. The app side is built (`IMPLEMENTATION.md` §4.16), and the `holdmytrack` collective applied to Open Source Collective as fiscal host on 2026-09-24; what's left: once approved, set the slug — `services/server/internal/web/web.go`'s `OpenCollectiveSlug` — and replace the About page template's "donations are not open yet" line with a link to it.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free, funded by its users" as credible before the public launch.
- [ ] DPIA before any public launch — drafted in `docs/DPIA.md`; what's left is its section 7 pre-launch actions and the controller's sign-off.
- [ ] EU-region hosting for EU users.
- [x] A privacy policy page — `/privacy` (`SPEC.md` FR-10.6). It names the email delivery provider only generically; name it there once settled, and update the policy when EU-region hosting moves the data.
- [x] Account deletion and data export — Settings' Delete account and Download your data, on the web and in the Android app (`SPEC.md` FR-1.11, FR-1.12). Named "Download your data" so it doesn't collide with the map's Export control (FR-4.10).
- [x] Health Connect data-type declarations in the Play Console, scoped to only what's actually used — filed 2026-10-07.

---

## Milestone 2 — Release and community

Starts at the public launch. Its exit gate is a measured answer to what community funding can carry: cost per active user (`IMPLEMENTATION.md` §4.32 keeps the active counts), set against monthly donations and the share of active users who give, over several months.

### Cost control

An engineering requirement, can land alongside any other work (`IMPLEMENTATION.md` §5.7).

- [ ] Serve the basemap as `/{z}/{x}/{y}` tiles Cloudflare can cache, `pmtiles serve` on the droplet first: today every range read into the archive reaches R2, since the free plan caches files only up to 512 MB (`IMPLEMENTATION.md` §5.4). Worth it once traffic grows; the zone saw about 97,000 basemap requests in the 30 days to 2026-10-14, under 1% of R2's free reads. The web style (`apps/web/src/map/style.ts`) and the style the server builds for Android (`mapstyle.go`) move from `pmtiles://` to the tile URL.
- [ ] Re-measure the funding-model assumptions (`VISION.md` §4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold — they feed Milestone 2's exit gate.

### After launch

- [ ] Review how long original uploads are kept, soon after the public launch, once real accounts show what they cost: today they're kept until the activity is deleted. They're the most sensitive thing held (`DPIA.md` R2) and likely the largest per-activity storage line, and an expired one can no longer be re-clipped after a Private location change, rebuilt by a track edit, or exported as the original (`IMPLEMENTATION.md` §5.7).
- [ ] The iOS app (`VISION.md` §5.3), likely after the public launch, with no date set: first confirm `HKWorkoutRoute` access with a throwaway app (`VISION.md` §4.1); then HealthKit sync, with `HKWorkoutRoute` for full GPS geometry, on the payload and the pick-what-to-send design Android settled (`SPEC.md` FR-3.6), and the site's own pages in a `WKWebView` for Settings, Profile, the Sync page and About (ADR-0042); then in-app GPS recording, on the Android build's wire shape and `source` convention (`SPEC.md` FR-3.8, ADR-0007).

---

## Milestone 3 — Social graph (deliberately not committed)

Public pages, followers and the rest (`VISION.md` §5.7, §5.8). No steps are listed on purpose: the first step is the Milestone 3 decision, made from Milestone 2's numbers, and not building it is one of its outcomes.

---

## Ongoing

- [ ] Describe the rest of the `/v1` JSON endpoints in `services/server/api/openapi.yaml` (`IMPLEMENTATION.md` §4.31), a few at a time: any test that already calls one checks it from then on.
- [ ] Decide one error shape for the JSON API — plain text from `http.Error`, or `{"error","message"}` everywhere — and describe it in the contract.
- [ ] Generate the web client's types from the contract (`openapi-typescript`) in place of `apps/web/src/api.ts`'s hand-written `*Body` interfaces.
- [ ] Move the Android app's `net/HoldMyTrackApi.kt` from hand-parsed `org.json` to typed data classes, checked against the contract.
- [ ] Check the Russian translation — run the Android app in Russian on a real device, and have a native speaker review the Russian across the web, the server's pages and emails, and the app (`IMPLEMENTATION.md` §4.21).
- [ ] Split `apps/web/src/map/MapView.tsx` by feature when the next feature touches it, the part that feature needs first: its state rules — Story mode, the Pending poll, track editing, selection and focus — into a reducer or a few custom hooks, under `verify:map` and the unit tests.
- [ ] Split the Android app's `MapFragment.kt` by feature when the next feature touches it, the part that feature needs first: the session and first-run check, Story mode, View on map, the notice panel, camera framing, photo markers — pure logic out first, with unit tests as `panel/PanelState` has, the UI wiring last.
- [ ] Turn on Sign in with Facebook (`SPEC.md` FR-1.10): Meta publishes the app only after Business Verification, which needs a business document such as an EIN letter or a DBA filing (`docs/DEPLOY.md` §4). Not a launch gate.