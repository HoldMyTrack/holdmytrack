# FitMap for Android — Roadmap

This document outlines the engineering and product roadmap for the native Android application. The Android app's primary responsibility is to serve as an on-device ingest path (Path 2, `docs/adr/0001-three-independent-ingest-paths.md`), reading platform health data via **Health Connect** and displaying the shared map interface using **MapLibre Native**.

As stated in `apps/android/README.md`, the Android development environment is not containerized and runs directly on the host machine.

Nothing here is built yet: `apps/android/` contains this file and a README.

## Where this sits in the wider plan

Read `docs/ROADMAP.md` (Phase 2 — Mobile) first; this document expands one item of it and does not override it.

- **Android ships first of the two mobile apps**, so the Path 2 contract is designed against the more constrained platform. Android defines the payload shape and the sync-cursor semantics that `POST /v1/sync/activities` accepts, and iOS inherits them. It is also the platform that produces route-less activities, so representing those end to end is part of this phase rather than a later one. Every server-side prerequisite below is on the critical path.
- **The design freeze is Phase 3 of the root roadmap, after Mobile.** There is no icon set, type scale, or token system yet, and the root plan itself flags this ordering as diverging from `VISION.md` §5 and "not yet reconciled". The screens below — onboarding, the honest-degrade state, the sync dashboard — fall inside that pass. Either build them plainly and style them there, or schedule the UI-heavy items after the freeze.
- **Cross-source deduplication is part of this phase, not a later one.** Root `ROADMAP.md` calls it "unavoidable once a second ingest source exists, which mobile sync is" — see Phase 4 below.

---

## Technical Foundations & Platform Constraints

Before implementing any UI or sync routines, we must design around two hard platform constraints. Both are already verified against platform and vendor documentation in `docs/IMPLEMENTATION.md` §4.0 — this section does not re-derive them.

1. **Foreground-Only Route Sync**
   - **Constraint**: `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it manually in Health Connect settings. Routes written by other apps cannot be read in the background — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow" granted.
   - **Decision**: Android sync is strictly **foreground-only** (`docs/IMPLEMENTATION.md` §4.0). We will not build background route sync. The specific trap this avoids is named there: a background sync "advances the watermark past records whose routes were never read", producing a history that is complete in every respect except the map — worse than no sync at all.

2. **Samsung Health Route-Geometry Limitation**
   - **Constraint**: Samsung Health writes summary data but does not expose GPS route geometry (`EXERCISE_ROUTE`) to other apps via Health Connect, so Galaxy Watch sync yields summary metrics and no map (`docs/VISION.md` §4.1, `docs/IMPLEMENTATION.md` §4.0).
   - **Decision**: Degrade honestly (`docs/IMPLEMENTATION.md` §5.1). A session with no route must be visibly marked "no route" rather than silently contributing nothing to the map; the failure mode to avoid is a user syncing 400 activities and seeing an empty map with no explanation.

---

## Server-side prerequisites

These are **not Android work**, but no Android phase can complete without them, and none of them exist today. Each is listed against the phase it blocks so the dependency is visible when scheduling.

- [ ] **Serve the basemap style as a document from the API** — *blocks Phase 2.*
  `docs/ARCHITECTURE.md` §2.1 is an instruction, not a description of something shipped: "Serve the style as a document from the API rather than reimplementing it per client — three hand-maintained copies of a 71-layer style would diverge." Today `buildStyle()` is an in-process TypeScript function (`apps/web/src/map/style.ts`) and no style route is registered in `services/server/internal/httpapi/server.go`. Android has nothing to consume until this exists.
- [ ] **Decide how MapLibre Native reads the basemap archive** — *blocks Phase 2.*
  The style's basemap source is a `pmtiles://` URL answered by the pmtiles **JavaScript** protocol plugin (`apps/web/src/map/style.ts`). MapLibre Native has no equivalent registered protocol, so this needs its own answer — a native PMTiles reader, or a server-side `{z}/{x}/{y}` endpoint over the archive. This is the largest open technical question in the Android plan and it is a shared-infrastructure decision, since the headless export renderer (`docs/IMPLEMENTATION.md` §5.5) hits the same wall.
- [ ] **Build `POST /v1/sync/activities`** — *blocks Phase 4.*
  Specced in `docs/IMPLEMENTATION.md` §4.0 ("batched normalized points, Path 2") and unimplemented. The only ingest endpoint that exists is `POST /v1/activities/upload` — multipart, a single `.gpx`/`.fit`/`.tcx`/`.zip` file per request (Path 3). Must be idempotent on `(user_id, source, external_id)` like the other two, per §4.0's hard invariant.
- [ ] **Accept route-less activities through ingest** — *blocks Phase 3's honest-degrade UI.*
  The destination state already exists — `activities.trajectory` is nullable (`docs/IMPLEMENTATION.md` §3.3) and `ActivitiesPanel.tsx` already renders "No track recorded for this activity" when `bbox === null` — but no ingest path can currently produce such a row: `internal/parse/gpx.go` and `tcx.go` both reject input with no positioned track points, and §4.0.2 there lists importing no-GPS activities as explicitly not built. Samsung's summary-only sessions therefore cannot be represented today. Honest degradation is an ingest requirement before it is a UI one.
- [ ] **Decide the mobile auth surface** — *blocks Phase 2.*
  Sessions are an opaque `fitmap_session` cookie holding a `sessions` row id, 30-day TTL, with an explicit browser-origin CORS allowlist (`internal/httpapi/auth.go`). There is no bearer-token path. A native client can hold the cookie in its own jar, but that is a decision to make deliberately — including how it interacts with `Secure` and with session invalidation on password reset — not an implementation detail to discover in Phase 4.

---

## Phase 1: Research & Verification

One throwaway app, one physical device, both platform questions answered together. Framed as due diligence rather than an open blocker: root `ROADMAP.md` notes that Samsung's own developer docs already state `EXERCISE_ROUTE` is unreachable, and this is "double-checking in case reality is better than documented". Both checks need the same device and the same permission plumbing, and the Samsung answer changes what Phase 3 has to build, so neither can wait until after the app exists.

- [ ] **Health Connect route-access proof of concept**
  - Test permission requests, verify the foreground route-reading requirement, and confirm that background reads return `ConsentRequired` (`docs/IMPLEMENTATION.md` §4.0).
- [ ] **Empirical Samsung Health check**
  - On a real Galaxy Watch paired to Samsung Health, confirm whether route geometry is genuinely unreachable via Health Connect, or whether there is any supported route we have missed (`docs/VISION.md` §4.1's validation gate). If routes turn out to be readable, Android's product improves materially and Phase 3's honest-degrade work shrinks to an edge case.

---

## Phase 2: Client Shell, Session & Map

Set up the application scaffolding and map rendering. Auth comes first within this phase, not later: every tile route is wrapped in `requireAuth` (`services/server/internal/httpapi/server.go`), so no user-specific tile renders without a live session.

- [ ] **Project setup & scaffolding**
  - Create a Kotlin/Android Studio project. Set `minSdk` and `targetSdk` deliberately and separately: `minSdk` decides whether the pre-Android-14 Health Connect APK path must be handled at all (Health Connect is built into the platform from Android 14 / API 34, a separately-installed APK below that), while `targetSdk` tracks Play's annual requirement and must be checked against the current one rather than inherited from this document.
  - Integrate MapLibre Native for Android.
- [ ] **Authentication & session persistence**
  - Implement login/signup against `POST /v1/auth/signup|login|logout` and `GET /v1/auth/me`, persisting the session per the prerequisite decision above.
  - MapLibre Native must attach the session to its *own* tile requests, which bypass the app's API client entirely — the same problem the web client solves with `transformRequest` in `useMapInstance.ts`.
- [ ] **Shared basemap style consumption**
  - Consume the style document served by the API (`docs/ARCHITECTURE.md` §2.1), and read the basemap archive by whichever mechanism the PMTiles prerequisite settles on.
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
- [ ] **Honest degrade UI**
  - Implement the UI state for Samsung Health activities and any others missing GPS data. Mirror the treatment the web client already gives a null-trajectory activity ("No track recorded for this activity") rather than inventing a second vocabulary for the same condition, and make sure the explanation reaches the user at sync time, not only in a row tooltip.
- [ ] **Payload preparation**
  - Standardize the on-device activity payload against whatever `POST /v1/sync/activities` accepts, including the summary-only, no-geometry case.

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
