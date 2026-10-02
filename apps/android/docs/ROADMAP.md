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
   - **Route-less activities are ordinary, and indoor exercise is why.** A gym session, a swim or a rowing machine has no trajectory by its nature — `docs/IMPLEMENTATION.md` §4.0.2 already meets the same case from the Takeout side and skips it there too. Measured on a real store (`docs/IMPLEMENTATION.md` §4.0), half the sessions had no geometry on exactly these grounds, and all of Samsung's do, regardless of activity type. HoldMyTrack's scope is outdoor GPS tracking, so these are skipped by design rather than represented — a gap to close later would only make sense for a product that also wants to be a general fitness tracker, which this one deliberately isn't.

---

## Phase 1: Research & Verification

Framed as due diligence rather than an open blocker: root `ROADMAP.md` notes that Samsung's own developer docs already state `EXERCISE_ROUTE` is unreachable, and this is "double-checking in case reality is better than documented".

- [ ] **Empirical Samsung Health check** — needs a Galaxy Watch paired to Samsung Health, which the Pixel the app was verified on can't stand in for.
  - On a real Galaxy Watch paired to Samsung Health, confirm whether route geometry is genuinely unreachable via Health Connect, or whether there is any supported route we have missed (`docs/VISION.md` §4.1's validation gate). If routes turn out to be readable, Android's product improves materially and Samsung Galaxy Watch stops being unsupported.

---

## Phase 6: Verification & Compliance (Pre-Launch)

Everything between a tested app and its Play Store listing, roughly in the order it has to happen. Requirements below were checked against Google's Play Console Help and Android developer documentation on 2026-10-02; Play's rules change, so recheck each against its current page when you reach it.

- [x] **Physical device / Health Connect testing**
  - Done end to end on a physical device, with the Health Connect toolbox on the host.
- [ ] **A Play Console developer account**
  - Personal or organization, decided first, since it changes what follows. A personal account created after 13 November 2023 can't publish to production until a closed test has had at least 12 testers opted in continuously for 14 days (Play Console Help, "App testing requirements for new personal developer accounts"); an organization account skips that but needs a D-U-N-S number for the organization. Either way: the one-time registration fee, identity verification, and, to distribute in the EU, a Digital Services Act trader-status declaration.
- [ ] **A release build**
  - `holdmytrack.apiBaseUrl` set to `https://holdmytrack.com` for release: `gradle.properties`' default is the emulator's `http://10.0.2.2:8080`, so a release built without `-P` points at nothing.
  - An upload key kept outside the repository, a `release` `signingConfig` that reads it, and `./gradlew bundleRelease`: Play takes an Android App Bundle, not an APK, and with Play App Signing it re-signs the app with a key Google holds. `release` has `isMinifyEnabled = false` today; R8 is optional, and would need MapLibre's and OkHttp's keep rules checked.
  - Already met, checked 2026-10-02: `targetSdk = 37` is above Play's requirement (API 36 for new apps and updates from 31 August 2026), and MapLibre's native libraries pass the 16 KB page-size check (`zipalign -c -P 16 -v 4` on the debug APK: every `libmaplibre.so` OK).
- [ ] **Google sign-in in the Play build**
  - Credential Manager only answers for an app whose signing certificate is registered on the Android OAuth client, and only this Mac's debug key is today. Add the SHA-1 of Play's app signing key (Play Console → App integrity), and the upload key's for any build installed outside Play. Facebook needs nothing: its sign-in is a browser-tab handoff to the server (ADR-0016), with no app signature involved.
- [x] **No donation link in the Play build**
  - Play's Payments policy lets an app take or point to payment only through Google Play's billing system, with an exception for donations to tax-exempt organizations; Play Billing itself doesn't sell donations. StreetComplete's Play submission was rejected for its Patreon, Liberapay and GitHub Sponsors links, and it took them out of its Play build. So the burger menu's Donate is in debug builds only, which the website's APK is (`BuildConfig.DONATE_LINK`, `apps/android/docs/IMPLEMENTATION.md` §8), and the release build, the Play one, has none.
  - StreetComplete's link to its project page was flagged as well, because that page carried the donation details. About, Help and Contacts open the web's pages, whose header has its own Donate: today it goes to `/about#funding`, which has no payment link ("donations are not open yet"), but once `OpenCollectiveSlug` (`services/server/internal/web/web.go`) is set it goes to Open Collective, from every one of those pages. Recheck before each Play submission once donations open.
- [ ] **A privacy policy page**
  - None exists yet: the server's pages are About, Help, Contacts and the account pages. Play requires one on the store listing, and the Data safety form and Health Connect declaration point to it. Health Connect also requires the policy on the listing to match the one users reach from Health Connect's link to the app, which opens `SyncActivity` (the manifest's `ACTION_SHOW_PERMISSIONS_RATIONALE` and `VIEW_PERMISSION_USAGE` entries), so that screen links to it too.
- [ ] **Account deletion, in the app and on the web**
  - Play requires both for any app where an account can be created: a path inside the app, and a web page where deletion can be requested, declared in the Data safety form. Blocked on the server's account deletion, root `ROADMAP.md` Phase 6.
- [ ] **Play Store Health Connect data-type declarations**
  - The list is settled and is as short as it can be: **one data type, Exercise** (`READ_EXERCISE`), plus `READ_EXERCISE_ROUTES` and `READ_HEALTH_DATA_HISTORY`, neither of which is an additional type — routes are part of an exercise session, and history is a time window over it. Confirmed on the device: Health Connect's own permission dialog for HoldMyTrack offers exactly one toggle. No heart rate, distance, calories, sleep or weight; the product draws outdoor GPS routes, so anything else would be requesting more than it demonstrably uses, which is a known rejection cause (`docs/VISION.md` §7; root `ROADMAP.md` Phase 6). What remains is filling in the Play Console declaration itself.
  - Filed as the Health apps declaration on Play Console's App content page: Activity and fitness, with a justification for each permission.
- [ ] **Location-permission Play Console declarations**
  - In-app recording's live location is a distinct Play Console review surface from Health Connect's data-type declarations above — location permissions have their own policy requirements (a prominent in-app disclosure before the first request, a stated retention/use case) that the Health Connect declarations don't cover.
  - `RecordingService` declares `foregroundServiceType="location"`, so the Foreground service permissions declaration is needed as well: a description of the feature, what the user loses if it's deferred or interrupted, and a video showing how a recording is started.
- [ ] **The rest of the App content page**
  - The Data safety form: email address, precise location, exercise sessions and routes, and photos are collected; all of it travels encrypted; none of it is shared or used for ads; accounts can be deleted.
  - The content rating questionnaire (IARC), the target audience (not aimed at children), and no ads.
  - App access: reviewers need a way in. The demo account gets them onto the map, but it can't sync Health Connect or edit anything, so supply a review account's credentials and say what's in it.
- [ ] **The store listing, in English and Russian**
  - Name, short description (80 characters), full description (4,000), the 512 × 512 icon (`brand/make_icons.py` renders the launcher icon from the same logo), a 1024 × 500 feature graphic, and at least two phone screenshots, plus a category, a contact email, the website and the privacy policy URL.
- [ ] **Testing tracks, then production**
  - An internal test first, to install the Play-signed build and check Google sign-in against its key. Then the closed test, with its 12 testers for 14 days on a personal account, and its pre-launch report read. Then apply for production access and release in stages.
- [ ] **Confirm the wider launch gates are met**
  - A Play Store release is a public launch and is gated by the same items as any other: the DPIA, EU-region hosting for EU users, and working data export and deletion endpoints (root `ROADMAP.md` Phase 6), plus the funding page and concept-render validation in its Phase 1. These are not Android work, but shipping the app without them is not an option.