# FitMap for Android — Roadmap

This document outlines the engineering and product roadmap for the native Android application. The Android app's primary responsibility is to serve as an on-device ingest path (Path 2, `docs/adr/0001-three-independent-ingest-paths.md`), reading platform health data via **Health Connect** and displaying the shared map interface using **MapLibre Native**.

As stated in `apps/android/README.md`, the Android development environment is not containerized and runs directly on the host machine.

`apps/android/` holds two Gradle builds: `fitmap/`, the app itself, and `poc-healthconnect/`, the throwaway Phase 1 diagnostic that is deleted once its findings are all recorded here. Some of the server-side work the app depends on is built — see the prerequisites below.

## Where this sits in the wider plan

Read `docs/ROADMAP.md` (Phase 2 — Mobile) first; this document expands one item of it and does not override it.

- **The toolchain is on the host, not in a container** (`apps/android/README.md`), and it is installed: the Android SDK at `/opt/homebrew/share/android-commandlinetools` with platform 37, build-tools 37 and `adb` under `platform-tools/`, alongside JDK 21, which the Android Gradle Plugin supports. `ANDROID_HOME` is not exported by default, so export it (or write `sdk.dir` into `local.properties`) before invoking `./gradlew`. There is no Android Studio and no emulator system image, so anything that needs a screen runs on a physical device over `adb`.
- **Android ships first of the two mobile apps**, so the Path 2 contract is designed against the more constrained platform. Android defines the payload shape and the sync-cursor semantics that `POST /v1/sync/activities` accepts, and iOS inherits them. Every server-side prerequisite below is on the critical path.
- **The design freeze is Phase 3 of the root roadmap, after Mobile.** There is no icon set, type scale, or token system yet, and the root plan itself flags this ordering as diverging from `VISION.md` §5 and "not yet reconciled". The screens below — onboarding, sync rejection feedback, the sync dashboard — fall inside that pass. Either build them plainly and style them there, or schedule the UI-heavy items after the freeze.
- **Cross-source deduplication is part of this phase, not a later one.** Root `ROADMAP.md` calls it "unavoidable once a second ingest source exists, which mobile sync is" — see Phase 4 below.

---

## Technical Foundations & Platform Constraints

Before implementing any UI or sync routines, we must design around two hard platform constraints. Both are already verified against platform and vendor documentation in `docs/IMPLEMENTATION.md` §4.0 — this section does not re-derive them.

1. **Foreground-Only Route Sync**
   - **Constraint**: `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it manually in Health Connect settings. Routes written by other apps cannot be read in the background — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow" granted.
   - **Decision**: Android sync is strictly **foreground-only** (`docs/IMPLEMENTATION.md` §4.0). We will not build background route sync. The specific trap this avoids is named there: a background sync "advances the watermark past records whose routes were never read", producing a history that is complete in every respect except the map — worse than no sync at all.

2. **Samsung Health Route-Geometry Limitation**
   - **Constraint**: Samsung Health writes summary data but does not expose GPS route geometry (`EXERCISE_ROUTE`) to other apps via Health Connect, so Galaxy Watch sessions never carry a route (`docs/VISION.md` §4.1, `docs/IMPLEMENTATION.md` §4.0).
   - **Decision**: FitMap only ingests activities that have a route — this is a deliberate product boundary (`docs/VISION.md` §1.1), not just a platform limitation to work around. A session with no geometry is rejected at sync time with a clear reason (`POST /v1/sync/activities` already does this — see the server-side prerequisites below), not persisted and not silently dropped. In practice this means **Samsung Galaxy Watch sync is unsupported**: every Samsung-sourced session arrives with no geometry and is therefore always rejected.
   - **Route-less activities are ordinary, and indoor exercise is why.** A gym session, a swim or a rowing machine has no trajectory by its nature — `docs/IMPLEMENTATION.md` §4.0.2 already meets the same case from the Takeout side and skips it there too. Measured on a real store (Phase 1 below), half the sessions had no geometry on exactly these grounds, and all of Samsung's do, regardless of activity type. FitMap's scope is outdoor GPS tracking, so these are skipped by design rather than represented — a gap to close later would only make sense for a product that also wants to be a general fitness tracker, which this one deliberately isn't.

---

## Server-side prerequisites

These are **not Android work**, but no Android phase can complete without them. Each is listed against the phase it blocks so the dependency is visible when scheduling.

- [x] **Serve the basemap style as a document from the API** (`docs/ARCHITECTURE.md` §2.1).
  `GET /v1/map/style/{flavor}` serves it, for the five flavors `style.ts` defines. `buildStyle()` remains the only definition of the style: `apps/web/scripts/build-style.mjs` renders it to JSON, `services/server/internal/mapstyle` embeds and serves that, and `npm run verify:style` fails on drift between the two. Asset URLs resolve against `BASEMAP_ORIGIN` (defaulting to `APP_BASE_URL`) at request time. Unauthenticated, and `ETag`/`If-None-Match` revalidation is wired up so a phone re-fetches ~100 KB of layer definitions only when it actually changed.
- [x] **Build `POST /v1/sync/activities`** (`docs/IMPLEMENTATION.md` §4.0.3).
  Takes a JSON batch of `{external_id, activity_type, points[]}`, `source` one of `"healthconnect"`/`"healthkit"`. Idempotent on `(user_id, source, external_id)` using the client-supplied `external_id` directly, not a content hash — unlike Path 3's file upload, Path 2 already carries a stable platform id. Reuses the existing ingest pipeline unchanged: `internal/parse` gained a `.json` case (`ParseJSON`) so `ingest.Process` needs no Path-2-specific branch, and `persistAndEnqueue` gained an optional caller-supplied `ExternalID`. One bad activity in a batch is rejected individually rather than failing the whole request, mirroring `handleZipUpload`'s per-entry treatment. Verified live against a running `db`/`minio`/`api`/`worker` stack: synced a 3-point run, watched it reach `state = 'done'` and show up in both `GET /v1/activities` and `GET /v1/uploads`, confirmed the persisted `raw_payload_key` is scoped `raw/{userID}/healthconnect/{externalID}.json` rather than content-addressed, and confirmed a retried sync of the same `external_id` returns `"already_processed"` rather than a duplicate row.
- [ ] **Decide the mobile auth surface** — *blocks Phase 2. Decided: bearer token, reusing the session table; server support not yet built.*
  Sessions are an opaque `fitmap_session` cookie holding a `sessions` row id, 30-day TTL, validated purely by `currentUserID` reading `r.Cookie(sessionCookieName)` (`internal/httpapi/auth.go`). There is no bearer-token path today. The CORS allowlist (`corsAllowedOrigins`, `server.go`) is irrelevant to this decision either way — CORS is a browser-enforced concept a native HTTP stack (OkHttp, `URLSession`) is never subject to, so mobile was never going to need adding to it.
  **Decision: `Authorization: Bearer <session-id>`, reusing the same `sessions` table** — not a new token system, just a second way to present the same credential `currentUserID` already validates. The deciding factor wasn't whether a cookie jar could work (it can — both OkHttp and `URLSession` support one natively) but that the mobile app has *two* separate HTTP surfaces needing the credential: its own API client, and MapLibre Native's own tile-fetching layer, which bypasses the app's client entirely (see "Confirm MapLibre Native's built-in PMTiles support" and "Shared basemap style consumption" in Phase 2). A cookie sitting in the app's own cookie jar doesn't automatically reach MapLibre Native's separate internal network stack; a bearer token injected via a per-request hook reaches both, uniformly:
  - **Android**: `org.maplibre.android.module.http.HttpRequestUtil.setOkHttpClient(...)` — supply an `OkHttpClient` built with a custom `Interceptor` that adds the header to every request MapLibre Native makes. Confirmed as a core-SDK API, not a React-Native-only shim, by reading `maplibre-react-native`'s own `TransformRequestInterceptor.kt`/`MLRNTransformRequestModule.kt`, which call exactly this.
  - **iOS**: `MLNNetworkConfiguration.sharedManager.delegate`, implementing `MLNNetworkConfigurationDelegate`'s `willSendRequest:` — same mutate-and-return shape as the web SDK's `transformRequest`, confirmed public/exported (`MLN_EXPORT`) in MapLibre Native's own `platform/darwin/src/MLNNetworkConfiguration.h`. This supersedes an older, fragile iOS mechanism (`MLNCustomHeaders`/`addCustomHeader`, method swizzling) with a known, unresolved upstream bug where the header silently failed to attach specifically on tile requests in release/TestFlight builds while working fine in dev — a plain delegate-protocol method isn't swizzling and isn't subject to that failure class.

  Both hooks are global to the app process (one interceptor/delegate for every request MapLibre Native makes, not scoped per source) — a non-issue here, since the only two destinations are FitMap's own tile API (wants the token) and the public pmtiles archive (an extra header is harmless there). Both run per-request rather than once at startup, so they read whatever token is current at call time — login/logout/refresh flow through without re-registering anything.
  **Not yet built**: `requireAuth`/`currentUserID` need to accept `Authorization: Bearer <session-id>` as an alternate lookup path alongside the existing cookie check — same `sessions` table, same expiry check, just a second place to read the id from. Password reset's existing `DELETE FROM sessions WHERE user_id = $1` already revokes a bearer token exactly the way it revokes a cookie, since both are just the same session row read two different ways. `Secure`/`SameSite` cookie attributes are moot for mobile, since mobile never exercises the cookie path at all.

---

## Phase 1: Research & Verification

One throwaway app, one physical device, both platform questions answered together. Framed as due diligence rather than an open blocker: root `ROADMAP.md` notes that Samsung's own developer docs already state `EXERCISE_ROUTE` is unreachable, and this is "double-checking in case reality is better than documented". Both checks need the same device and the same permission plumbing, and the Samsung answer changes what Phase 3 has to build, so neither can wait until after the app exists.

- [x] **Health Connect route-access proof of concept** — `apps/android/poc-healthconnect`, measured on a Pixel 10a running Android 17 (API 37) against a Health Connect store fed by Fitbit.
  - **`READ_EXERCISE_ROUTES` is not programmatically requestable.** Requesting it alongside `READ_EXERCISE` and `READ_HEALTH_DATA_IN_BACKGROUND` grants the other two and silently omits it — it never even acquires a `USER_SET` flag. The user grants it at **Health Connect → the app → Additional access → Access exercise routes → Always allow**, a screen two levels below the app's main permission page and not linked from it. Phase 3's onboarding has to walk the user there explicitly; "grant permissions" is not a single flow.
  - **Foreground-only is real, and "Always allow" does not lift it.** With all three permissions granted, the same query over the same 46 sessions returned `23 route / 23 no-route / 0 consent-required` in the foreground and `0 / 23 / 23` in the background. Background access being granted changes nothing for routes.
  - **`NoData` and `ConsentRequired` are distinguishable, and both are stable.** 23 of the 46 sessions read as `NoData` in both runs — indoor workouts, which have no route to begin with — while the outdoor half flipped between geometry and `ConsentRequired` depending only on foreground state. So the two conditions never have to be conflated: a rejected sync can say "recorded indoors, no route" or "route not readable right now, sync again in the foreground" as the different things they are, rather than one generic "couldn't sync" message.
- [ ] **Empirical Samsung Health check** — not doable on the Pixel used above; needs a Galaxy Watch paired to Samsung Health.
  - On a real Galaxy Watch paired to Samsung Health, confirm whether route geometry is genuinely unreachable via Health Connect, or whether there is any supported route we have missed (`docs/VISION.md` §4.1's validation gate). If routes turn out to be readable, Android's product improves materially and Samsung Galaxy Watch stops being unsupported.

---

## Phase 2: Client Shell, Session & Map

Set up the application scaffolding and map rendering. Auth comes first within this phase, not later: every tile route is wrapped in `requireAuth` (`services/server/internal/httpapi/server.go`), so no user-specific tile renders without a live session.

- [x] **Project setup & scaffolding** — `apps/android/fitmap`, a Kotlin/Gradle project with MapLibre Native 13.6.1 drawing the API's style document full-screen. Its own build rather than a module beside `poc-healthconnect`, which is throwaway; see `apps/android/fitmap/README.md` for how to build it and point it at an API. `./gradlew :app:assembleDebug` packages it against the host SDK; what it looks like on a screen is the next item's business, since a map with no session is not yet a useful thing to photograph.
  - **`minSdk` is 34, `targetSdk` 37**, decided separately as this item asks. 34 (Android 14) is where Health Connect became part of the platform: below it, Health Connect is a Play-installed APK the app must detect, route the user into installing, and re-check — a second provider state machine, on a configuration Phase 1 never measured routes against. FitMap ingests GPS sessions from a watch, so the devices that cut off are not the ones it serves. 37 is the newest platform and matches `compileSdk`, comfortably above Play's current requirement of 36; that requirement rises annually and is re-checked per release rather than read off this document.
  - The shell renders before any account exists, which is why it comes first: the style endpoint is unauthenticated and the basemap archive is a plain `pmtiles://` URL, so the map is on screen before there is a session to attach. Every *user* layer is behind `requireAuth` and arrives with the next item. Flavor follows the system day/night setting — `light` or `dark` of the five the API serves — rather than inventing a theme preference the design pass has not decided.
  - Failures surface on screen, not only in logcat: a style or tile fetch that 404s otherwise leaves a plausible-looking blank map with nothing to say what went wrong, which is the failure mode every remaining item in this phase will hit while being wired up.
- [ ] **Authentication & session persistence**
  - Implement login/signup against `POST /v1/auth/signup|login|logout` and `GET /v1/auth/me`, persisting the session per the prerequisite decision above.
  - MapLibre Native must attach the session to its *own* tile requests, which bypass the app's API client entirely — the same problem the web client solves with `transformRequest` in `useMapInstance.ts`.
- [ ] **Confirm MapLibre Native's built-in PMTiles support** — *likely already resolved, needs live confirmation.*
  The style document (`docs/ARCHITECTURE.md` §2.1) carries a `pmtiles://` source URL, answered on web by the pmtiles **JavaScript** protocol plugin (`apps/web/src/map/style.ts`). This was believed to need its own answer for mobile — a native PMTiles reader to build, or a server-side `{z}/{x}/{y}` endpoint over the archive — because MapLibre Native had no equivalent registered protocol. That's now out of date: MapLibre Native gained **built-in, native `pmtiles://` support** (Android 11.8.0, iOS 6.10.0), the same URL scheme the web client already uses, refined across several later releases (range-request support and a forced XYZ tile scheme in Android 11.8.8, better tile-compression handling in 13.0.2, an ambient cache in 13.3.0). Neither of the two options once thought necessary here looks needed anymore — the library already does it.
  Not checked off yet because this is documentation research, not the live verification this project holds itself to elsewhere (same standard as Phase 1's Samsung check): confirm against a real build once the toolchain exists, pointed at FitMap's own archive, and specifically re-check whether "PMTiles sources do not support offline pack downloads or caching" (MapLibre's own docs, apparently pre-dating the 13.3.0 ambient cache) still holds in practice. If it does resolve cleanly, the headless export renderer's identical question (`docs/IMPLEMENTATION.md` §5.5) is very likely resolved along with it, if that renderer ends up running on MapLibre Native rather than a browser.
- [ ] **Shared basemap style consumption**
  - Consume the style document served by the API (`docs/ARCHITECTURE.md` §2.1), and read the basemap archive via MapLibre Native's own `pmtiles://` support (see the confirmation item above).
- [ ] **Map modes: Normal, Fog of War, Heatmap**
  - Fog and heatmap are server-rendered PNG rasters (`GET /tiles/v1/fog|heatmap/{z}/{x}/{y}`). Normal mode is not a server-rendered tile: it is the basemap plus the tracks vector layer (`GET /tiles/v1/tracks/{z}/{x}/{y}`, live PostGIS→MVT), with both rasters hidden. All three accept `from`/`to`/`types`/`exclude` filters.

---

## Phase 3: Health Connect Ingestion (Path 2)

Implement permissions, local tracking, and sync logic.

- [ ] **Permission management & onboarding flow**
  - Walk the user through granting Health Connect permission and explain the manual step for `READ_EXERCISE_ROUTES`, which cannot be requested programmatically.
- [ ] **Foreground sync service**
  - Implement the sync worker that reads workouts and routes only while the app is in the foreground.
- [ ] **Sync cursor / watermark**
  - Maintain the sync watermark such that it never advances past a record whose route was not actually read (`docs/IMPLEMENTATION.md` §4.0). This is the correctness requirement that makes foreground-only sync viable; simple "don't double-submit" bookkeeping is not sufficient, and idempotency on `(user_id, source, external_id)` is enforced server-side regardless.
- [ ] **Sync rejection feedback**
  - Health Connect sessions with no route (Samsung Health, or any activity Health Connect can't hand a route for) are rejected by `POST /v1/sync/activities`, not persisted and not retried forever. Surface the per-activity rejected status and reason from Phase 4's sync status/error dashboard, so the user understands why a session never appeared — no separate map/list UI is needed for activities that never exist in the database.
- [ ] **Payload preparation**
  - Standardize the on-device activity payload against what `POST /v1/sync/activities` accepts (`docs/IMPLEMENTATION.md` §4.0.3).

---

## Phase 4: API Integration & Sync Progress

Connect the native application to the FitMap backend.

- [ ] **Sync API client**
  - Push synced activities to `POST /v1/sync/activities` in batches, with resumable retry — `docs/IMPLEMENTATION.md` §4.0 notes mobile uploads get interrupted as a matter of course, and retries are normal rather than exceptional.
- [ ] **Cross-source deduplication** (`docs/IMPLEMENTATION.md` §4.6, root `ROADMAP.md` Phase 2)
  - Android sync is the second ingest source to exist, which is the point at which this stops being optional. The design is already fixed: `dedupe_key` at ingest (user, activity type, start time rounded to the nearest minute, distance bucketed to ~1%), prefer the richest record when two collide, mark the others `superseded` rather than deleting them. Server-side work, triggered by this phase.
- [ ] **Sync status & error dashboard**
  - Show sync history, pending uploads, and status/error detail for failed attempts.

---

## Phase 5: Verification & Compliance (Pre-Launch)

Prepare the app for testing and store publication.

- [ ] **Physical device / Health Connect testing**
  - End-to-end testing on a physical device with the Health Connect toolbox on the host.
- [ ] **Play Store Health Connect data-type declarations**
  - Prepare the exact list of Health Connect data types for the Play Console declaration, scoped strictly to what the product demonstrably uses — requesting more types than that is a known rejection cause (`docs/VISION.md` §7; root `ROADMAP.md` Phase 6).
- [ ] **Confirm the wider launch gates are met**
  - A Play Store release is a public launch and is gated by the same items as any other: the DPIA, EU-region hosting for EU users, and working data export and deletion endpoints (root `ROADMAP.md` Phase 6), plus the funding page and concept-render validation in its Phase 1. These are not Android work, but shipping the app without them is not an option.
