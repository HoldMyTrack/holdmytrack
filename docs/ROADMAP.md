# Roadmap

Last updated: 2026-09-20.

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

**Shipped and deployable.** Every feature in `SPEC.md`'s FR-1 through FR-9 — auth and account management, the no-signup demo, activity upload/ingestion (file, `.zip`, Google Takeout), Normal/Fog of War/Heatmap map modes with colored zone segments and high-res export, the Activities panel and its filters, the date-range picker, the per-account activity graph, per-activity pace/heart-rate, and distance trends — is built and documented there; not re-enumerated here.

### Auto-set Country at signup from a coarse IP lookup — shared with the demo preset library above and the map's zero-history fallback (`SPEC.md` FR-4.5, `IMPLEMENTATION.md` §4.13)

Today `users.country` (FR-1.7) is set only if a person visits Settings and picks one — leaving it unset silently defaults the whole app to metric (§FR-1.7.3), including for e.g. a US-based signup who never thinks to check Settings, and means the map's zero-history fallback has no Country to fall back to for most new accounts either (it still reaches its final `WORLD_VIEW` tier correctly, just one tier further out than it needs to for an account this would have covered).

- [ ] Resolve country from the signup request's IP address (a self-hosted database like MaxMind's free GeoLite2 — no external API call needed, no extra request from the client) and set `users.country` at account-creation time, instead of leaving it null. Same remote-address extraction the demo rate limiter already does (`IMPLEMENTATION.md` §4.10) — build it once as shared server-side logic, used here and by the demo preset library above, rather than two separate lookups. The map's zero-history fallback needs no changes to benefit — it already reads whatever `users.country` holds, however it got set.
- [ ] Stays a normal, editable Settings field afterward — never locked. Country-level accuracy from a database like GeoLite2 is good but not perfect (VPNs, corporate networks, travel), so this is a sensible default, not an authoritative fact about the account.
- [ ] Browser locale (`Accept-Language`) and the browser Geolocation API were both considered and rejected as the signal here: locale reflects a language/OS preference, not physical location (someone with `en-GB` set while living elsewhere gets the wrong answer), and the Geolocation API needs an explicit permission prompt — exactly the kind of signup friction the email-verification item above is trying to avoid elsewhere. IP geolocation needs neither.
- [ ] If a CDN ever fronts the app (`Production deployment`'s open CDN item), and if that CDN happens to be one that injects a country header on every request (e.g. Cloudflare's `CF-IPCountry`), that becomes a free replacement for the self-hosted lookup — worth revisiting then, not a blocker now.

### Signed-out web map browsing — `apps/android/docs/SPEC.md`'s "no login wall" framing isn't actually true of the web client

Discovered while building the signed-in zero-history fallback chain (now shipped, `SPEC.md` FR-4.5, `IMPLEMENTATION.md` §4.13): `App.tsx`'s signed-out branch renders only `AuthGate` (the login/signup form) — `MapView` never mounts without an active session (FR-4.1's own "Preconditions: Active session"). A signed-out visitor today has no map to see at all, not merely a generic one. This was previously assumed, incorrectly, to already work (an earlier version of this file's wording implied a signed-out fallback point already existed) — it doesn't, and `docs/ARCHITECTURE.md` itself makes no "signed-out FitMap is a working map" claim for web; that line exists only in the Android docs.

- [ ] Decide whether web should offer real anonymous map browsing (a public basemap, no personal data, matching Android's own stated design) — and if so, build it: `MapView`/`AuthProvider` tolerating no session, a real render for `auth === 'signed-out'`, and an opening view for it (likely `WORLD_VIEW`, `config.ts` — the same constant the signed-in zero-history fallback already ends at).
- [ ] If the answer is no — web stays login-gated by design, unlike Android — update `AGENTS.md`/`ARCHITECTURE.md`'s cross-references so nothing implies otherwise for web specifically.

### "View on map" from a finished upload — closes the gap between "an upload just finished processing" and actually seeing it, for anything beyond a single file

The fly-to-most-recent-activity item above only fires once, on first load — it does nothing for an upload made *during* an already-open session. That's an easy manual fix for one file (adjust the date picker, click the row), but there is no equivalent for a `.zip`/Google Takeout batch of many files: uploads are already fully async per-file (`handleUpload`'s own doc comment: "No parsing happens here" — every file, single or batched, is enqueued as its own `ingest` job and picked up later by `cmd/fitmap work`), and the panel already polls `GET /v1/uploads` every 1.5s tracking each file's own `processing`/`done`/`failed` status (`useUploadHistory`) — but nothing today does anything with a given file's *completion* beyond a blanket "something finished, reload everything" (`onPoll` -> `handleUploaded()`). There is no "multi-focus" concept, and inventing one (e.g. auto-checking every newly-finished activity and auto-flying to fit the whole batch) was considered and rejected: it can't tell "the user is watching and wants this" from "the user alt-tabbed away to do something else entirely, possibly on a slow connection with a long upload still running in the background" — moving their camera and selection state for them regardless would be actively hostile in the second, much more likely case for a multi-file import.

- [ ] Instead, make it fully opt-in and per-file: once an upload row in the panel reaches `done`, show a small "View on map" action on that row — reusing the exact focus mechanism a row click in the Activities list already triggers (bold on the map, fly to it, and scroll the list to center that row — already shipped, `ActivitiesPanel.tsx`'s `scrollIntoView({ block: 'center' })` effect), just reachable from a second place, and never triggered without a click.
- [ ] `GET /v1/uploads` (`uploads.go`'s `uploadRow`) needs to actually carry the resulting activity's id — today the LEFT JOIN to `activities` only surfaces `started_at`/`distance_meters` for the "READY · 11 Sep · 8.2 km" line, not `a.id`, so there is currently nothing for a click handler to focus.
- [ ] Because the Activities list is scoped to the currently selected date range, focusing an activity whose day isn't in that range has to first narrow the date picker to that day (same mechanism `changeSelectedRange` already uses for a manual drag, marking it user-driven so the FR-6.1 default-range effect doesn't fight it), *then* focus once that range's refetch actually includes the target id — a two-step async sequence, not a single call, since the range change and the fly both depend on a fetch landing first.

### FitMap logo watermark on exports — free brand exposure, drawn in the same pass as a separately-tracked attribution fix

`docs/KNOWN_ISSUES.md` tracks a compliance bug on this same code path (exported PNGs currently carry no OSM/Protomaps attribution at all, despite `style.ts:28`'s own comment stating it's required on every export). This item is the feature half of that `exportMap.ts` change, not the bug fix itself: a small FitMap logo/wordmark, worth landing in the same pass since both need to be baked into the raster itself before `toBlob()`, not just shown via the live map's DOM-based `AttributionControl`, which the export path bypasses entirely.

- [ ] Add a small FitMap logo/wordmark in a non-competing corner of the export (sharing the same lower-corner strip the attribution fix will use, small and low-contrast, never covering map content) — free brand exposure on exports that get shared, which fits `VISION.md` §6's free-forever, donation-funded, no-ad-budget model. Several free, sharing-driven apps (Strava, Peloton, Duolingo) put the same kind of subtle mark on their own shareable images for the same reason.
- [ ] On by default, with a simple toggle to turn it off per export (or a persisted preference) — a courtesy, not a paywall gate, since there is no paid tier here to protect. Unlike the attribution fix (a license requirement, not a preference), this one is genuinely optional.

### Export requires something to export, plus a WYSIWYG-or-fit choice — FR-4.10 today exports whatever shape the screen happens to produce, with no way to ask for a different one

Today's export (`exportMap.ts`) fixes width at 2400px and derives height from the *live browser viewport's own* `clientHeight`/`clientWidth` ratio — so the exact same view exported from a wide desktop monitor versus a narrow window versus a phone browser comes out as three differently-shaped images, purely by accident of the screen it was exported from, never a deliberate choice. Two other designs were considered and rejected before landing on the one below: an arbitrary aspect-ratio picker applied to the current viewport has no clean answer once the target ratio is very different from the source (e.g. ultrawide desktop → mobile portrait) — holding zoom fixed either pads the result with empty space or crops content off the sides, and re-fitting the viewport's own bounding box doesn't help either, since a wide-zoomed-out viewport often has a lot of empty map in it that just becomes empty padding in the new shape too. The fix is to stop trying to reshape an incidental viewport at all, and instead always target *real content* (a selection), with two clearly different, non-overlapping options for how to frame it.

- [ ] **Export requires at least one activity to export.** The target is the checked group (FR-5.6) or row-click focus (FR-5.5) if either is active; otherwise it falls back to the full currently-filtered list (the same set already driving the visible tracks or the Fog/Heatmap raster) — so Export stays available exactly as often as it is today (the moment anything is loaded and visible), and checking specific rows only narrows it. This matters most in Fog of War/Heatmap mode, where the natural thing to export is "my whole coverage so far," not a handful of manually-checked rows — requiring an explicit per-activity selection there would be a real regression.
- [ ] **"WYSIWYG"**: exports exactly the current on-screen framing (same center, zoom, and aspect ratio your screen currently has) — no cropping, no reshaping. If part of a selected activity is currently outside the viewport, it simply isn't in the export, the same way it isn't currently on your screen; nothing is trimmed after the fact. The only change from today: output resolution is capped by **scaling proportionally so the longer of width/height hits a fixed target** (e.g. 2400px) and the shorter side scales down with it, rather than today's fixed-width-2400px rule, which can produce disproportionately large output on an unusually tall or narrow window.
- [ ] **A small set of fixed-shape presets** (square, portrait, landscape, shown as icons rather than raw dimensions), each of which fits the *selection's own bounding box* — never the viewport's — fully inside the chosen shape, reusing the same `fitBounds`-style computation `flyToBBox` already provides elsewhere. Fitting the content's own tight bounds, rather than whatever happened to be on screen, is what actually guarantees every selected activity is fully visible, with no cropping and no needlessly tiny result.
- [ ] Everything else about FR-4.10 (theme, mode, date-range/hidden-track filters reflected, the busy state, the `fitmap-{date}.png` naming, the attribution/logo baked in per `docs/KNOWN_ISSUES.md`'s attribution fix and the logo item above) is unchanged and shared across every option — this only changes what bounding box and shape each option targets.
- [ ] **Open question: rename the control itself.** "Export" no longer fits well once it's selection-based and framed around producing a shareable image — and Phase 6's planned GDPR data-export feature will also want the word "Export" for something completely different (downloading your raw account data), which would collide with this if both keep the same name. Candidates raised: "Share," "Save," or something else — not decided; whoever builds this should pick a name and rename the FR-4.10 header control and the `fitmap-{date}.png` download accordingly.

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

### CI on push/PR — closes the gap where `tsc`/Go tests/`make test` already exist but nothing runs them automatically

Now that `main` feeds a real, if small, live deployment (`freefitmap.com`), a broken build or a regression slipping past manual local verification is a materially bigger risk than it was pre-deployment. `tsc --noEmit`, `make test` (the web's `verify:map`/`verify:build`), and four Go unit test files (`dedupe_test.go`, `parse_test.go`, `fit_test.go`, `mapstyle_test.go`) already exist, but nothing runs them except whoever remembers to type the command locally — there is no `.github/workflows` at all today, despite the repo already living on GitHub and already using PRs.

- [ ] A GitHub Actions workflow running on every push/PR: `tsc --noEmit`, `go test ./...`, and `make test` — all three already exist and already pass locally; this is wiring, not new test-writing.
- [ ] Fold `go test ./...` into `make test` itself (or a sibling target) while at it — right now the Go tests aren't part of any single command, automated or not, so even a careful local run before pushing can miss them.
- [ ] Deliberately not a large new unit-test-writing effort — this project's stated quality approach favors manual, live verification over unit tests (`IMPLEMENTATION.md`'s repeated "confirmed live, not just a unit test", `DEVELOPMENT.md`'s verification checklist); this item wires up checks that already exist, it doesn't change that philosophy.
- [ ] **CD (auto-deploy on merge) is explicitly out of scope here, and gated behind the Production deployment section's backups item above.** Auto-deploying every merge onto the one uncopied copy of real synced health data, with no restore path if a bad deploy corrupts something, is a bigger risk than the manual deploy step it would replace. Revisit once backups and a restore drill exist.

### Pre-launch validation — gates any public launch, regardless of which paths are live

- [ ] Stand up the funding page (Open Collective, public ledger — `VISION.md` §6.1) before any public launch, not retrofitted after.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §5.1, §8.1) — validate "free forever, funded by users" as credible before building further.

---

## Phase 2 — Mobile

Native apps whose core job is exporting device-recorded health data to FitMap (Path 2 on-device sync, `docs/adr/0001-three-independent-ingest-paths.md`) — reading the platform's own health store rather than a cloud API. Needed no licensing gate the way Phase 4's cloud connectors do: HealthKit/Health Connect access itself isn't in question, only how much of it (see the two confirmation items below, which are this phase's own first steps, not an external blocker).

- [ ] Confirm the Samsung Health Connect route-geometry limitation empirically, not just from documentation (`VISION.md` §4.1) — Samsung's own developer docs already state `EXERCISE_ROUTE` cannot be read via Health Connect; this is double-checking in case reality is better than documented, not an open question blocking the build.
- [ ] Confirm `HKWorkoutRoute` access with a throwaway iOS app (`VISION.md` §4.1) — due diligence before committing engineering effort to the full iOS build, not resolving a real unknown: Apple's docs already say this works.
- [x] Android app: Health Connect sync, foreground-only (`READ_EXERCISE_ROUTES` can't be requested programmatically, and background route reads return `ConsentRequired` even with "Always allow" granted — a platform constraint, not an implementation shortcut). Samsung Galaxy Watch is unsupported — it never exposes route geometry, and FitMap only ingests activities that have one (the confirmation item above is about verifying that limitation firsthand, not about whether sync itself works). Built first as planned, so the Path 2 sync contract (payload shape, sync-cursor semantics) was designed against the harder platform's constraints, ready for iOS to inherit. `apps/android/docs/ROADMAP.md` (35 of its own 53 items checked, Phases 2–4 and 7) carries this app's remaining work (UI design freeze, Play Store compliance) — this root item tracks only "does Path 2 sync itself work end to end," which it now does (`docs/SPEC.md` FR-3.6).
- [ ] iOS app: HealthKit sync, `HKWorkoutRoute` for full GPS geometry — the stronger of the two on-device paths, and the one that inherits the payload and sync-cursor design Android settles.
- [x] In-app GPS recording, Android half — a convenience capture for casual, watch-free activities (a road trip, a dog walk), not a fitness-tracker replacement: GPS only, no sensor data, no training metrics (`VISION.md` §4.1, §1.1). Submits directly through the existing ingest pipeline once a recording stops, reusing the sync endpoint's payload shape rather than opening a new one ([ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)) — no new server-side path, just a new `source` value. `apps/android/docs/ROADMAP.md` Phase 7 carries the detail; `docs/SPEC.md` FR-3.8 is the behavior spec.
- [ ] In-app GPS recording, iOS half — inherits the Android build's wire shape and `source` convention once the iOS app itself exists (see the iOS Path 2 item above, which this depends on).

### Rename "Upload" to "Import", split into Files/Sync tabs — the current single-purpose upload UI stops being an accurate label the moment a second ingest source exists

`UploadPanel.tsx`'s "Upload activity" button/panel is the only ingest surface in the web app today, since manual file/`.zip`/Google Takeout upload is the only source shipped (`SPEC.md` FR-1 through FR-9). This phase's mobile sync and Phase 4's cloud connectors both add ingest sources that aren't "a file the user picked," so "Upload" stops describing what's actually happening the moment either lands.

- [ ] Rename the entry point from "Upload activity" to "Import", with two tabs: **Files** (today's drag/drop + `.zip`/Takeout flow, unchanged) and **Sync** (status for whichever automatic sources are connected).
- [ ] The Sync tab is a status/connected-accounts view, not a "sync now" button, for Health Connect/HealthKit specifically — sync there is phone-triggered (`READ_EXERCISE_ROUTES` can't be requested programmatically off-app, this phase's own item above), so the web app can only ever report something like "last synced via Android app: 2h ago, 3 new activities," reusing the same per-item status list `useUploadHistory`/`GET /v1/uploads` already renders for file uploads (processing/done/failed) rather than inventing a second status UI.
- [ ] Phase 4's cloud connectors (Garmin/Wahoo/COROS) are the one part of "Sync" that *is* triggerable from this tab directly — connect/disconnect, and (once that phase's OAuth token-refresh runs on a schedule) the same last-synced/status summary as the on-device sources above.
- [ ] Not worth building ahead of either dependency landing — registered here so the IA decision (naming, the tab split, and which half of Sync is a button vs. a status display) gets made once, deliberately, instead of retrofitted under time pressure once this phase or Phase 4 actually ships.

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

- [ ] Keep `SPEC.md`/`IMPLEMENTATION.md` in sync with each change, per the cross-reference discipline already established (a change to one almost always means a small edit to the other). `AGENTS.md` is orientation only, not a status narrative — it doesn't need a matching edit just because a feature changed.
- [ ] Re-measure the funding-model assumptions (§4.3, §6.3) against real usage once any real users exist, rather than assuming the estimates hold.