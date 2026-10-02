# HoldMyTrack for Android — Roadmap

This document outlines the engineering and product roadmap for the native Android application. The Android app's primary responsibility is to serve as an on-device ingest path (Path 2, `docs/adr/0001-three-independent-ingest-paths.md`), reading platform health data via **Health Connect** and displaying the shared map interface using **MapLibre Native**. It also has a second, unrelated way for an activity to originate: casual, GPS-only recording done directly in HoldMyTrack, submitted straight to the server rather than through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md), `docs/VISION.md` §4.1).

As stated in `apps/android/README.md`, the Android development environment is not containerized and runs directly on the host machine.

`apps/android/holdmytrack/` is the app's Gradle build. Every server-side prerequisite the app depends on is built.

## Where this sits in the wider plan

Read `docs/ROADMAP.md` (Phase 2 — Mobile) first; this document expands one item of it and does not override it.

- **The toolchain is on the host, not in a container** (`apps/android/README.md`), and it is installed: the Android SDK at `/opt/homebrew/share/android-commandlinetools` with platform 37, build-tools 37 and `adb` under `platform-tools/`, alongside JDK 21, which the Android Gradle Plugin supports. `ANDROID_HOME` is not exported by default, so export it (or write `sdk.dir` into `local.properties`) before invoking `./gradlew`. There is no Android Studio, but an AVD now exists (`holdmytrack`, a Pixel 7 image on Android 16/API 36, under `emulator/`) — used to verify in-app recording (`apps/android/docs/SPEC.md` §9); FR-1 through FR-4's own verification record still names a physical device specifically, and that distinction is worth preserving rather than blurring the two.
- **Android ships first of the two mobile apps**, so the Path 2 contract is designed against the more constrained platform. Android defines the payload shape and the sync-cursor semantics that `POST /v1/sync/activities` accepts, and iOS inherits them.
- **The design is frozen, product-wide** (root `docs/ROADMAP.md` Phase 3, `VISION.md` §5.4). The app's look is the web's tokens — palette, Inter and Fraunces, and the spacing, radius and type scales — and its Lucide icons, carried into Material 3 (`apps/android/docs/IMPLEMENTATION.md` §1.3, [ADR-0026](../../../docs/adr/0026-material-3-on-views.md)). The visual design comes *from* the root pass rather than being decided again here: two clients that each invented their own palette and type scale would not read as one product.
- **Cross-source deduplication is part of this phase, not a later one.** Root `ROADMAP.md` calls it "unavoidable once a second ingest source exists, which mobile sync is" — built, `docs/IMPLEMENTATION.md` §4.6.

---

## Technical Foundations & Platform Constraints

Before implementing any UI or sync routines, we must design around two hard platform constraints. Both are already verified against platform and vendor documentation in `docs/IMPLEMENTATION.md` §4.0 — this section does not re-derive them.

1. **Foreground-Only Route Sync**
   - **Constraint**: `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it manually in Health Connect settings. Routes written by other apps cannot be read in the background — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow" granted.
   - **Decision**: Android sync is strictly **foreground-only** (`docs/IMPLEMENTATION.md` §4.0). We will not build background route sync. The specific trap this avoids is named there: a background sync "advances the watermark past records whose routes were never read", producing a history that is complete in every respect except the map — worse than no sync at all.
2. **Samsung Health Route-Geometry Limitation**
   - **Constraint**: Samsung Health writes summary data but does not expose GPS route geometry (`EXERCISE_ROUTE`) to other apps via Health Connect, so Galaxy Watch sessions never carry a route (`docs/VISION.md` §4.1, `docs/IMPLEMENTATION.md` §4.0).
   - **Decision**: HoldMyTrack only ingests activities that have a route — this is a deliberate product boundary (`docs/VISION.md` §1.1), not just a platform limitation to work around. A session with no geometry is rejected at sync time with a clear reason (`POST /v1/sync/activities` already does this — see the server-side prerequisites below), not persisted and not silently dropped. In practice this means **Samsung Galaxy Watch sync is unsupported**: every Samsung-sourced session arrives with no geometry and is therefore always rejected.
   - **Route-less activities are ordinary, and indoor exercise is why.** A gym session, a swim or a rowing machine has no trajectory by its nature — `docs/IMPLEMENTATION.md` §4.0.2 already meets the same case from the Takeout side and skips it there too. Measured on a real store (Phase 1 below), half the sessions had no geometry on exactly these grounds, and all of Samsung's do, regardless of activity type. HoldMyTrack's scope is outdoor GPS tracking, so these are skipped by design rather than represented — a gap to close later would only make sense for a product that also wants to be a general fitness tracker, which this one deliberately isn't.

---

## Phase 1: Research & Verification

One throwaway app, one physical device, both platform questions answered together. Framed as due diligence rather than an open blocker: root `ROADMAP.md` notes that Samsung's own developer docs already state `EXERCISE_ROUTE` is unreachable, and this is "double-checking in case reality is better than documented". Both checks need the same device and the same permission plumbing, and the Samsung answer changes what Phase 3 has to build, so neither can wait until after the app exists.

- [x] **Health Connect route-access proof of concept** — a throwaway diagnostic app, measured on a Pixel 10a running Android 17 (API 37) against a Health Connect store fed by Fitbit.
  - **`READ_EXERCISE_ROUTES` is not programmatically requestable.** Requesting it alongside `READ_EXERCISE` and `READ_HEALTH_DATA_IN_BACKGROUND` grants the other two and silently omits it — it never even acquires a `USER_SET` flag. The user grants it at **Health Connect → the app → Additional access → Access exercise routes → Always allow**, a screen two levels below the app's main permission page and not linked from it. Phase 3's onboarding has to walk the user there explicitly; "grant permissions" is not a single flow.
  - **Foreground-only is real, and "Always allow" does not lift it.** With all three permissions granted, the same query over the same 46 sessions returned `23 route / 23 no-route / 0 consent-required` in the foreground and `0 / 23 / 23` in the background. Background access being granted changes nothing for routes.
  - **`NoData` and `ConsentRequired` are distinguishable, and both are stable.** 23 of the 46 sessions read as `NoData` in both runs — indoor workouts, which have no route to begin with — while the outdoor half flipped between geometry and `ConsentRequired` depending only on foreground state. So the two conditions never have to be conflated: a rejected sync can say "recorded indoors, no route" or "route not readable right now, sync again in the foreground" as the different things they are, rather than one generic "couldn't sync" message.
- [ ] **Empirical Samsung Health check** — not doable on the Pixel used above; needs a Galaxy Watch paired to Samsung Health.
  - On a real Galaxy Watch paired to Samsung Health, confirm whether route geometry is genuinely unreachable via Health Connect, or whether there is any supported route we have missed (`docs/VISION.md` §4.1's validation gate). If routes turn out to be readable, Android's product improves materially and Samsung Galaxy Watch stops being unsupported.

---

## Phase 6: Verification & Compliance (Pre-Launch)

Prepare the app for testing and store publication.

- [x] **Physical device / Health Connect testing**
  - Done end to end on a physical device, with the Health Connect toolbox on the host.
- [ ] **Play Store Health Connect data-type declarations**
  - The list is settled and is as short as it can be: **one data type, Exercise** (`READ_EXERCISE`), plus `READ_EXERCISE_ROUTES` and `READ_HEALTH_DATA_HISTORY`, neither of which is an additional type — routes are part of an exercise session, and history is a time window over it. Confirmed on the device: Health Connect's own permission dialog for HoldMyTrack offers exactly one toggle. No heart rate, distance, calories, sleep or weight; the product draws outdoor GPS routes, so anything else would be requesting more than it demonstrably uses, which is a known rejection cause (`docs/VISION.md` §7; root `ROADMAP.md` Phase 6). What remains is filling in the Play Console declaration itself.
- [ ] **Location-permission Play Console declarations**
  - In-app recording's live location is a distinct Play Console review surface from Health Connect's data-type declarations above — location permissions have their own policy requirements (a prominent in-app disclosure before the first request, a stated retention/use case) that the Health Connect declarations don't cover.
- [ ] **Confirm the wider launch gates are met**
  - A Play Store release is a public launch and is gated by the same items as any other: the DPIA, EU-region hosting for EU users, and working data export and deletion endpoints (root `ROADMAP.md` Phase 6), plus the funding page and concept-render validation in its Phase 1. These are not Android work, but shipping the app without them is not an option.

---

## Phase 11: Spots places refresh

Root `docs/ROADMAP.md` Phase 1's "Spots places refresh": a place gone from OpenStreetMap is retired, never deleted — hidden from everyone who hasn't captured it, still shown to those who have.

- [ ] **Hide retired places** — `map/MapSpots` leaves out a retired place's badge and area unless its id is in the account's captured ids, the list it already loads for the filled badge, as the web's `apps/web/src/map/spots.ts` will. Blocked on the server marking them: the spots tiles' `retired` flag, and `GET /v1/spots` returning a retired place only to an account that captured it.