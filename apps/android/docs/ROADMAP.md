# HoldMyTrack for Android — Roadmap

This document outlines the engineering and product roadmap for the native Android application. The Android app's primary responsibility is to serve as an on-device ingest path (Path 2, `docs/adr/0001-three-independent-ingest-paths.md`), reading platform health data via **Health Connect** and displaying the shared map interface using **MapLibre Native**. It also has a second, unrelated way for an activity to originate: casual, GPS-only recording done directly in HoldMyTrack, submitted straight to the server rather than through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md), `docs/VISION.md` §4.1).

As stated in `apps/android/README.md`, the Android development environment is not containerized and runs directly on the host machine.

`apps/android/holdmytrack/` is the app's Gradle build. Every server-side prerequisite the app depends on is built.

## Where this sits in the wider plan

Read `docs/VISION.md` §5.3 (Mobile) first; this document is the Android app's remaining work within it and does not override it.

- **The toolchain is on the host, not in a container** (`apps/android/README.md`), and it is installed: the Android SDK at `/opt/homebrew/share/android-commandlinetools` with platform 37, build-tools 37 and `adb` under `platform-tools/`, alongside JDK 21, which the Android Gradle Plugin supports. `ANDROID_HOME` is not exported by default, so export it (or write `sdk.dir` into `local.properties`) before invoking `./gradlew`. There is no Android Studio, but an AVD now exists (`holdmytrack`, a Pixel 7 image on Android 16/API 36, under `emulator/`) — used to verify in-app recording (`apps/android/docs/SPEC.md` §9); FR-1 through FR-4's own verification record still names a physical device specifically, and that distinction is worth preserving rather than blurring the two.
- **Android ships first of the two mobile apps** (`VISION.md` §5.3): it defines the payload shape `POST /v1/sync/activities` accepts and the pick-what-to-send model over `POST /v1/sync/known` (root `docs/SPEC.md` FR-3.6), and iOS inherits them.
- **The design is frozen, product-wide** (`VISION.md` §5.4). The app's look is the web's tokens — palette, Inter and Fraunces, and the spacing, radius and type scales — and its Lucide icons, carried into Material 3 (`apps/android/docs/IMPLEMENTATION.md` §1.3, `apps/android/docs/ARCHITECTURE.md` §1.1).

---

## Technical Foundations & Platform Constraints

Before implementing any UI or sync routines, we must design around two hard platform constraints. Both are already verified against platform and vendor documentation in `docs/IMPLEMENTATION.md` §4.0 — this section does not re-derive them.

1. **Foreground-Only Route Sync**
   - **Constraint**: `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it manually in Health Connect settings. Routes written by other apps cannot be read in the background — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow" granted.
   - **Decision**: Android sync is strictly **foreground-only** (`docs/IMPLEMENTATION.md` §4.0). We will not build background route sync. The specific trap this avoids is named there: a background sync would send activities whose routes were never read, producing a history that is complete in every respect except the map — worse than no sync at all.
2. **Samsung Health Route-Geometry Limitation**
   - **Constraint**: Samsung Health writes summary data but does not expose GPS route geometry (`EXERCISE_ROUTE`) to other apps via Health Connect, so Galaxy Watch sessions never carry a route (`docs/VISION.md` §4.1, `docs/IMPLEMENTATION.md` §4.0).
   - **Decision**: HoldMyTrack only ingests activities that have a route — this is a deliberate product boundary (`docs/VISION.md` §1.1), not just a platform limitation to work around. A session with no geometry is rejected at sync time with a clear reason (`POST /v1/sync/activities` already does this), not persisted and not silently dropped. In practice this means **Samsung Galaxy Watch sync is unsupported**: every Samsung-sourced session arrives with no geometry and is therefore always rejected.
   - **Route-less activities are ordinary, and indoor exercise is why.** A gym session, a swim or a rowing machine has no trajectory by its nature — Measured on a real store (`docs/IMPLEMENTATION.md` §4.0), half the sessions had no geometry on exactly these grounds, and all of Samsung's do, regardless of activity type. These are skipped by design (`VISION.md` §1.1).

---

## Ongoing

- [ ] **Testing tracks, then production**
  - Version 896 (`0.9`) is on the internal and closed testing tracks; Google sign-in against Play's key was checked on the internal track's earlier build. The Play Console account is personal, so it can't publish to production until a closed test has had at least 12 testers opted in continuously for 14 days (Play Console Help, "App testing requirements for new personal developer accounts"). Next, those testers, recruited through Testers Community, and the pre-launch report read. Testers are pointed at `https://holdmytrack.com/testing`, the step-by-step test suite with its sample files (root `docs/SPEC.md` FR-10.7). Then apply for production access and release in stages; a production release is a public launch, so it waits for root `ROADMAP.md`'s "Before launch".
  - Play's rules change: recheck the release requirements against Play Console Help before each submission.
- [ ] **The Digital Services Act trader-status declaration**, which the Play Console account needs before the app can be distributed in the EU.
- [ ] **No payment link reachable from the Play build, once donations open**
  - Play's Payments policy lets an app point to payment only through Google Play's billing, so the Play build has no Donate (`apps/android/docs/IMPLEMENTATION.md` §8). Help, Contacts and Privacy open the web's pages in a browser tab, whose header's Donate goes to Open Collective once `OpenCollectiveSlug` (`services/server/internal/web/web.go`) is set, and About's funding section would link there too. StreetComplete's Play submission was rejected for such a link, so recheck before each submission once donations open.
- [ ] **`WebPageActivity.route` under test**
  - It takes an Android `Uri`, so it has no unit test, though the first-run redirect bug lived in it. Move the decision into a function over plain values and test it, as `AGENTS.md`'s "Decisions in pure logic" asks of new code.