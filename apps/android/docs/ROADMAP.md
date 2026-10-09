# HoldMyTrack for Android — Roadmap

This document outlines the engineering and product roadmap for the native Android application. The Android app's primary responsibility is to serve as an on-device ingest path (Path 2, `docs/adr/0001-three-independent-ingest-paths.md`), reading platform health data via **Health Connect** and displaying the shared map interface using **MapLibre Native**. It also has a second, unrelated way for an activity to originate: casual, GPS-only recording done directly in HoldMyTrack, submitted straight to the server rather than through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md), `docs/VISION.md` §4.1).

As stated in `apps/android/README.md`, the Android development environment is not containerized and runs directly on the host machine.

`apps/android/holdmytrack/` is the app's Gradle build. Every server-side prerequisite the app depends on is built.

## Where this sits in the wider plan

Read `docs/ROADMAP.md` (Phase 2 — Mobile) first; this document expands one item of it and does not override it.

- **The toolchain is on the host, not in a container** (`apps/android/README.md`), and it is installed: the Android SDK at `/opt/homebrew/share/android-commandlinetools` with platform 37, build-tools 37 and `adb` under `platform-tools/`, alongside JDK 21, which the Android Gradle Plugin supports. `ANDROID_HOME` is not exported by default, so export it (or write `sdk.dir` into `local.properties`) before invoking `./gradlew`. There is no Android Studio, but an AVD now exists (`holdmytrack`, a Pixel 7 image on Android 16/API 36, under `emulator/`) — used to verify in-app recording (`apps/android/docs/SPEC.md` §9); FR-1 through FR-4's own verification record still names a physical device specifically, and that distinction is worth preserving rather than blurring the two.
- **Android ships first of the two mobile apps** (`VISION.md` §5.3): it defines the payload shape `POST /v1/sync/activities` accepts and the pick-what-to-send model over `POST /v1/sync/known` (root `docs/SPEC.md` FR-3.6), and iOS inherits them.
- **The design is frozen, product-wide** (`VISION.md` §5.4). The app's look is the web's tokens — palette, Inter and Fraunces, and the spacing, radius and type scales — and its Lucide icons, carried into Material 3 (`apps/android/docs/IMPLEMENTATION.md` §1.3, [ADR-0026](../../../docs/adr/0026-material-3-on-views.md)).

---

## Technical Foundations & Platform Constraints

Before implementing any UI or sync routines, we must design around two hard platform constraints. Both are already verified against platform and vendor documentation in `docs/IMPLEMENTATION.md` §4.0 — this section does not re-derive them.

1. **Foreground-Only Route Sync**
   - **Constraint**: `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it manually in Health Connect settings. Routes written by other apps cannot be read in the background — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow" granted.
   - **Decision**: Android sync is strictly **foreground-only** (`docs/IMPLEMENTATION.md` §4.0). We will not build background route sync. The specific trap this avoids is named there: a background sync would send activities whose routes were never read, producing a history that is complete in every respect except the map — worse than no sync at all.
2. **Samsung Health Route-Geometry Limitation**
   - **Constraint**: Samsung Health writes summary data but does not expose GPS route geometry (`EXERCISE_ROUTE`) to other apps via Health Connect, so Galaxy Watch sessions never carry a route (`docs/VISION.md` §4.1, `docs/IMPLEMENTATION.md` §4.0).
   - **Decision**: HoldMyTrack only ingests activities that have a route — this is a deliberate product boundary (`docs/VISION.md` §1.1), not just a platform limitation to work around. A session with no geometry is rejected at sync time with a clear reason (`POST /v1/sync/activities` already does this — see the server-side prerequisites below), not persisted and not silently dropped. In practice this means **Samsung Galaxy Watch sync is unsupported**: every Samsung-sourced session arrives with no geometry and is therefore always rejected.
   - **Route-less activities are ordinary, and indoor exercise is why.** A gym session, a swim or a rowing machine has no trajectory by its nature — `docs/IMPLEMENTATION.md` §4.0.2 already meets the same case from the Takeout side and skips it there too. Measured on a real store (`docs/IMPLEMENTATION.md` §4.0), half the sessions had no geometry on exactly these grounds, and all of Samsung's do, regardless of activity type. These are skipped by design (`VISION.md` §1.1).

---

## Phase 1: Research & Verification

Framed as due diligence rather than an open blocker: root `ROADMAP.md` notes that Samsung's own developer docs already state `EXERCISE_ROUTE` is unreachable, and this is "double-checking in case reality is better than documented".

- [ ] **Empirical Samsung Health check** — needs a Galaxy Watch paired to Samsung Health, which the Pixel the app was verified on can't stand in for.
  - On a real Galaxy Watch paired to Samsung Health, confirm whether route geometry is genuinely unreachable via Health Connect, or whether there is any supported route we have missed (`docs/VISION.md` §4.1, Path 2). If routes turn out to be readable, Android's product improves materially and Samsung Galaxy Watch stops being unsupported.

---

## Phase 6: Verification & Compliance (Pre-Launch)

Everything between a tested app and its Play Store listing, roughly in the order it has to happen. Requirements below were checked against Google's Play Console Help and Android developer documentation on 2026-10-02; Play's rules change, so recheck each against its current page when you reach it.

- [x] **Physical device / Health Connect testing**
  - Done end to end on a physical device, with the Health Connect toolbox on the host.
- [x] **A Play Console developer account**
  - A personal account, owned by `admin@holdmytrack.com`, registered and verified 2026-10-03, including the Android device check. Being personal, it can't publish to production until a closed test has had at least 12 testers opted in continuously for 14 days (Play Console Help, "App testing requirements for new personal developer accounts"). To distribute in the EU, it also needs the Digital Services Act trader-status declaration.
- [x] **A release build**
  - Release takes its API address from `holdmytrack.releaseApiBaseUrl`, `https://holdmytrack.com`, and is signed by a `release` `signingConfig` reading an upload key kept outside the repository; `./gradlew bundleRelease` makes the App Bundle Play takes, which Play App Signing re-signs with a key Google holds (`apps/android/holdmytrack/README.md`, Release build). Checked 2026-10-06: the bundle verifies with the upload key's certificate, and its `BuildConfig` has the production address and no Donate. `release` has `isMinifyEnabled = false`; R8 is optional, and would need MapLibre's and OkHttp's keep rules checked.
  - Already met, checked 2026-10-02: `targetSdk = 37` is above Play's requirement (API 36 for new apps and updates from 31 August 2026), and MapLibre's native libraries pass the 16 KB page-size check (`zipalign -c -P 16 -v 4` on the debug APK: every `libmaplibre.so` OK).
- [x] **Google sign-in in the Play build**
  - Credential Manager only answers for an app whose signing certificate is registered on an Android OAuth client, so Play's app signing key has one of its own, beside this Mac's debug key's; its SHA-1 is on Play Console's Protected with Play page, or in the signed universal APK App bundle explorer offers. A release build installed outside Play would need the upload key's too. Facebook needs nothing: its sign-in is a browser-tab handoff to the server (ADR-0016), with no app signature involved. Checked 2026-10-07: version 860, installed from the internal test track, signed in with Google.
- [x] **No donation link in the Play build**
  - Play's Payments policy lets an app take or point to payment only through Google Play's billing system, with an exception for donations to tax-exempt organizations; Play Billing itself doesn't sell donations. StreetComplete's Play submission was rejected for its Patreon, Liberapay and GitHub Sponsors links, and it took them out of its Play build. So the You tab's Donate is in debug builds only, which the website's APK is (`BuildConfig.DONATE_LINK`, `apps/android/docs/IMPLEMENTATION.md` §8), and the release build, the Play one, has none.
  - StreetComplete's link to its project page was flagged as well, because that page carried the donation details. About, Help, Contacts and Privacy open the web's pages, whose header has its own Donate: today it goes to `/about#funding`, which has no payment link ("donations are not open yet"), but once `OpenCollectiveSlug` (`services/server/internal/web/web.go`) is set it goes to Open Collective, from every one of those pages. Recheck before each Play submission once donations open.
- [x] **A privacy policy page**
  - `/privacy` (root `docs/SPEC.md` FR-10.6), linked from the You tab and from the Health Connect rationale (the Sync tab's, and `SyncActivity`, which Health Connect's own link to the app opens), so the policy users reach there is the one on the store listing.
- [x] **Account deletion, in the app and on the web**
  - In the app, Settings' Delete account (`apps/android/docs/SPEC.md` FR-1.5); on the web, Settings' Delete account section. The Data safety form's deletion URL is Help's `https://holdmytrack.com/help#delete-account`, which explains both and the email route for someone who can't sign in (root `docs/SPEC.md` FR-1.11).
- [x] **Play Store Health Connect data-type declarations**
  - The list is settled and is as short as it can be: **one data type, Exercise** (`READ_EXERCISE`), plus `READ_EXERCISE_ROUTES` and `READ_HEALTH_DATA_HISTORY`, neither of which is an additional type — routes are part of an exercise session, and history is a time window over it. Confirmed on the device: Health Connect's own permission dialog for HoldMyTrack offers exactly one toggle. No heart rate, distance, calories, sleep or weight; the product draws outdoor GPS routes, so anything else would be requesting more than it demonstrably uses, which is a known rejection cause (`docs/VISION.md` §7; root `ROADMAP.md` Phase 6).
  - Filed 2026-10-07 as the Health apps declaration on Play Console's App content page: Activity and fitness as the only feature, and a justification for each of the three permissions.
- [x] **The foreground-service declaration for recording**
  - No Location permissions declaration: Play asks for one only from an app targeting Android 10 or newer that declares `ACCESS_BACKGROUND_LOCATION` (Play Console Help, "Location permissions declaration", checked 2026-10-03), and recording is foreground-only, `ACCESS_FINE_LOCATION` asked for on the record button's first tap.
  - `RecordingService` declares `foregroundServiceType="location"`, so the Foreground service permissions declaration on the App content page is needed. Filed 2026-10-07 under Background location updates as Other, not User-initiated location sharing, since a recording goes to the user's own map and is shared with no one: a description of the feature, what the user loses if it's interrupted, and a link to a 60-second emulator video of a recording started from the record button, through the location dialog, the notification, the screen off and pause.
- [x] **The rest of the App content page**
  - Data safety: nothing shared; collected are the email address, user IDs and an optional name (account management), precise location, fitness info (Health Connect sessions), photos, files and docs (uploads), other user-generated content (names, descriptions, Stories, Private locations, captures), all for app functionality, and diagnostics (the server's logs and metrics, analytics); all of it travels encrypted, and an account or any part of its data can be deleted.
  - The content rating questionnaire (IARC), the target audience (16 and over, not aimed at children), no ads and no advertising ID.
  - Sign in details (App access): the demo can't sync, edit or upload, so reviewers get `review@holdmytrack.com`, a production account with eleven of the Demo Customer's GPX files uploaded and its email marked confirmed, and instructions for Health Connect sync and recording. Its password is in Play Console only.
  - Filled in 2026-10-07; sent for review from Publishing overview.
- [x] **The store listing, in English and Russian**
  - Name, short description (80 characters), full description (4,000), the 512 × 512 icon (`brand/make_icons.py` renders the launcher icon from the same logo), a 1024 × 500 feature graphic (`brand/play-feature-graphic.png`, and `-ru` for the Russian listing, rendered by the same script), and at least two phone screenshots, plus a category, a contact email, the website and the privacy policy URL. Entered 2026-10-07: Maps & Navigation, `hello@holdmytrack.com`, `https://holdmytrack.com`; eight phone, eight 7-inch and eight 10-inch tablet screenshots of the Demo Customer on production, alternating light and dark, which the Russian listing shares.
- [ ] **Testing tracks, then production**
  - Version 896 (`0.9`) is on the internal and closed testing tracks; Google sign-in against Play's key was checked on the internal track's earlier build. Next, the closed test's 12 testers opted in for 14 days on a personal account, recruited through Testers Community, and its pre-launch report read. Testers are pointed at `https://holdmytrack.com/testing`, the step-by-step test suite with its sample files (root `docs/SPEC.md` FR-10.7). Then apply for production access and release in stages.
- [ ] **Confirm the wider launch gates are met**
  - A Play Store release is a public launch and is gated by the same items as any other: the DPIA, EU-region hosting for EU users (root `ROADMAP.md` Phase 6), plus the rest of Milestone 2's entry gate (root `ROADMAP.md`, Milestones). These are not Android work, but shipping the app without them is not an option.

---

## Phase 7: Phone-first redesign

The map screen and its menus carry the web's phone layout over, which on a phone stacks the chrome in bands and hides the most-used screens behind a burger. This phase gives the app a layout of its own, on the same tokens, fonts and icons: a bottom navigation bar (Map, Stories, Record, Sync, You), one floating row at the map's top, a draggable sheet whose peek holds the date range, and track editing as a focused mode. The decision to make the tabs Fragments of one Activity is [ADR-0033](../../../docs/adr/0033-android-bottom-navigation-single-activity.md). Each item below is one pull request, each working on its own.

- [x] **The map as a Fragment**
  - `MapFragment` holds everything the map did, hosted by `MainActivity` with no change in behavior (`apps/android/docs/IMPLEMENTATION.md` §1.2). Checked on the emulator against a local stack: sign-in, rotation in Fog, a recording surviving rotation, the notification's Stop, a row selected from the panel, the burger menu, View on map, and a day/night switch.
- [x] **The bottom navigation bar**
  - Map, Stories, Record (the record button, raised in the middle), Sync and You (`apps/android/docs/SPEC.md` FR-2.9); Sync is the map's sheet on its Sync tab, its reads and runs cancelled the moment it leaves the foreground or another tab hides it; the burger menu is gone, and You lists what it held until it gets its own design. Checked on the emulator against a local stack, in English and Russian: each tab, Stories marking the map's Stories tab, Back to Map, a recording started from Sync landing on Map and stopped by a hold, rotation on Sync, and Health Connect's rationale opening the Sync screen on its own.
- [x] **The map's top chrome**
  - One row: Normal, Fog and Heatmap with icons, and Layers over Find my location on the right; Show layers and Satellite are switches in the Layers menu; the recording's status, with Stop, takes the toggle's place while recording; a chip says what Fog and Heatmap show (`apps/android/docs/SPEC.md` FR-2.2). Checked on the emulator against a local stack, in English and Russian: the row, the Layers menu, Fog's chip, and a recording's card with Stop confirmed.
- [x] **The sheet and the date scrubber**
  - A draggable sheet with collapsed, half and expanded heights; its head is the range, its totals and a day scrubber whose bars are each activity day's distance, paged by holding a handle past the edge; the tab row is gone, Stories and Private locations reached from the bottom bar and the You tab (`apps/android/docs/SPEC.md` FR-2.6, FR-2.7 item 1). Checked on the emulator against a local stack: the three heights by drag, handle and Back, a bar picked, ‹ shifting the range, Stories and Private locations, and rotation in both orientations.
- [x] **The selected activity and multi-select**
  - A selected activity's card at the sheet's head (distance, moving time, pace or speed, the pace bands' legend, its actions), Select making the rows checkable with the toolbar as a bar over the map, Type and Distance as chips, and rows with a tile of their kind (`apps/android/docs/SPEC.md` FR-2.7 items 2–7). Checked on the emulator against a local stack: a drive's card with its speed and legend, checking rows and the bar's partial master checkbox, the Distance popup narrowing the list, and × ending selecting.
- [x] **Track editing as a focused mode**
  - An edit bar (Cancel, the title, Save) in the top row's place and the Edit window along the bottom, the sheet and the bottom bar away; the Track tab's four tools as large icon buttons, Undo and Reset beside RANGE; the Private location editor in the same frame (`apps/android/docs/SPEC.md` FR-2.7 items 9, 12, 13; FR-2.9). Checked on the emulator against a local stack with a real account: a walk's start chopped and saved, reprocessed to 1.0 mi with its bands redrawn, and a new private location opened and cancelled.
- [x] **The Sync, You and Privacy screens**
  - Sync: what's on the phone, drawn on the map, to tick and send, Health Connect, Upload and Timeline, and the latest imports with the whole history a tap away; the bottom bar's Sync badged with the recordings waiting. You: the account, all-time totals leading to Activity graph & trends, Privacy, Theme and Language, the web's pages, Sign out and Delete account. Privacy: Private locations, edited on the map, what HoldMyTrack keeps, and Download your data (`apps/android/docs/SPEC.md` FR-1.7, FR-2.9, FR-3.5). Donate stays out of the Play build. Checked on the emulator against a local stack, in dark mode: Sync signed in and as the demo account, See all, You's card and Language set to Русский and back, the Delete account dialog, and a private location added, reopened and deleted.
- [x] **Polish and accessibility**
  - Every target 48dp, the record button's raised part included; Normal, Fog and Heatmap read as radio buttons; rows that don't fit a large font size stack, and the mode toggle keeps only the active label; the sheet's head whole in landscape, the camera kept across a rotation, theme or language change, and the content inset from a side navigation bar and the camera cutout (`apps/android/docs/SPEC.md` §8). Checked on the emulator against a local stack: a `uiautomator` audit of every screen at font scales 1.0 and 2.0, light and dark, Russian, and landscape with three-button navigation. TalkBack's spoken pass on a physical device is still to do.

## Phase 8: Web features to carry over

Features the web has that the app doesn't yet. The server side of each is built; only the app's own part is listed.

- [x] **Split an activity** (`docs/SPEC.md` FR-5.17, `docs/IMPLEMENTATION.md` §4.7.8)
  - A Split tool in the Track tab, its two-color preview, and the confirm for a part left inside a Private location (`apps/android/docs/SPEC.md` FR-2.7 item 12). Merge waits for the web's (`docs/ROADMAP.md`). Checked on the emulator against a local stack, in English and Russian: a walk ending inside a Private location split at a point inside it, the confirm, Split anyway, and two rows, one of them Private.
- [x] **The Private badge** (`docs/SPEC.md` FR-5.1)
  - A lock-and-"Private" badge in place of distance and duration, and its explanation on the selected activity's card in place of its stats (`apps/android/docs/SPEC.md` FR-2.7 item 2). Checked in the same run.