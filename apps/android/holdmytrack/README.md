# holdmytrack-android

The HoldMyTrack Android app (`apps/android/docs/ROADMAP.md`). It renders the map, holds a session, and syncs exercise sessions in from Health Connect: a full-screen MapLibre Native map drawing the style document the API serves at `GET /v1/map/style/{flavor}`, sign-in against the same accounts the web client uses, the Normal / Fog of War / Heatmap toggle over the user's own layers, and Path 2 on-device ingest into `POST /v1/sync/activities`.

The basemap draws whether or not anyone is signed in — the style endpoint is unauthenticated and the archive it points at is a plain `pmtiles://` URL that MapLibre Native reads natively — so a signed-out HoldMyTrack is a working map rather than a login wall. Signing in is what adds the three user layers, every one of which is behind `requireAuth` server-side.

## Build requirements

Nothing here is containerised, deliberately: the SDK, the emulator and the physical device Health Connect testing needs all live on the host.

- **JDK 17 or 21.** The Android Gradle Plugin supports no other versions.
- **Android SDK** with platform 37 and build-tools 37. On this machine it is Homebrew's `android-commandlinetools` at `/opt/homebrew/share/android-commandlinetools`, with `adb` in its `platform-tools/`.
- **`ANDROID_HOME` pointing at that SDK**, or an `sdk.dir` line in a `local.properties` next to this file. Neither is committed and Gradle fails without one of them.

```sh
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
./gradlew :app:assembleDebug     # APK in app/build/outputs/apk/debug/
./gradlew installDebug           # build and push to the attached device
```

Gradle itself is not a prerequisite — the committed wrapper (`./gradlew`) pins its own version.

## Pointing the app at an API

`BuildConfig.API_BASE_URL` is baked in at build time. A debug build takes it from the `holdmytrack.apiBaseUrl` Gradle property, which `gradle.properties` defaults to `http://10.0.2.2:8080` — the emulator's alias for the host's loopback, so it reaches a `make up` stack on the host. A release build takes `holdmytrack.releaseApiBaseUrl` instead, `https://holdmytrack.com` (see Release build below). Override the debug one for anything else:

```sh
# Physical device over USB, against a host stack whose API is published on 8081.
# The style document's own asset URLs resolve against BASEMAP_ORIGIN, which defaults to
# APP_BASE_URL (the web dev server on 5173) — so the basemap needs its port forwarded too.
adb reverse tcp:8081 tcp:8081
adb reverse tcp:5173 tcp:5173
./gradlew installDebug -Pholdmytrack.apiBaseUrl=http://127.0.0.1:8081

# Against production. The basemap comes from the style document's own URLs, so nothing
# else needs forwarding.
./gradlew assembleDebug -Pholdmytrack.apiBaseUrl=https://holdmytrack.com
```

Cleartext `http://` is permitted in debug builds only (`app/src/debug/AndroidManifest.xml`), so a release build cannot quietly ship pointing at one.

## Release build

The release build is the Play build: an Android App Bundle signed with the upload key, which Play App Signing re-signs with the key Google holds. The upload keystore and its passwords are kept out of the repository, in four properties in `~/.gradle/gradle.properties`:

```properties
holdmytrack.uploadStoreFile=/Users/<you>/.android/holdmytrack-upload.jks
holdmytrack.uploadStorePassword=…
holdmytrack.uploadKeyAlias=upload
holdmytrack.uploadKeyPassword=…
```

```sh
./gradlew bundleRelease   # app/build/outputs/bundle/release/app-release.aab
```

Without the properties, `bundleRelease` stops at `validateSigningRelease` ("Keystore file not set for signing config release"); debug builds don't need them. Keep a copy of the keystore and its password somewhere other than this machine: a lost upload key can be replaced only by asking Google to reset it from Play Console.

## Sign in with Google and Facebook

Both buttons appear only when the API the app points at has them configured (`GET /v1/auth/providers`).

- **Google** needs an Android OAuth client for your signing key in the same Google Cloud project as the server's web client (`docs/DEPLOY.md`, Sign in with Google, step 4). For a debug build, that's the SHA-1 of your debug key: `keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android -keypass android | grep SHA1`. Without it the account picker fails and the app logs `google sign-in failed` under the `SignInActivity` tag.
- **Facebook** runs in a browser tab against the server's own callback URL. Against a local stack, forward the API's port (`adb reverse tcp:8081 tcp:8081`) so the tab's `http://localhost:8081/v1/auth/facebook/callback` reaches it, and use a Facebook account with a role on the Meta app while it's in Development mode.

## Layout

Everything is under `app/src/main/kotlin/dev/holdmytrack/android/`:

- `HoldMyTrackApplication.kt` — process-level setup. The load-bearing line is `HttpRequestUtil.setOkHttpClient`, which replaces MapLibre Native's own HTTP client with the app's. The map SDK fetches the style, the archive and every tile through a stack the app's API client never sees, so without this the session would reach none of the user layers.
- `MainActivity.kt` — the main window: the session gate, and the bottom bar (Map, Stories, Record, Sync, You) over the tabs' fragments.
- `MapFragment.kt` — the map, the on-map mode toggle, the record button's behaviour, and MapLibre's lifecycle forwarding.
- `YouFragment.kt` — the You tab: the account and its numbers, Privacy, Theme and Language, the web's pages, Sign out and Delete account, and the version.
- `ProfileActivity.kt` — Activity graph & trends, from the You tab: the web's `/profile` page.
- `SignInActivity.kt` — sign in, create an account, or start a demo account.
- `net/Session.kt` — the session token, held process-wide and mirrored to private `SharedPreferences`.
- `net/HoldMyTrackApi.kt` — the whole HTTP surface: the shared `OkHttpClient`, the interceptor that attaches the token to HoldMyTrack's own origin and nowhere else, and the five calls the app makes.
- `map/MapOverlays.kt` — the tracks, fog and heatmap layers, their ordering beneath the basemap's labels, and the three-way mode toggle.
- `map/MapModeButton.kt` — a Normal, Fog or Heatmap button, read by TalkBack as one choice of three.
- `map/SyncCandidatesOverlay.kt` — the Sync tab's candidates on the map, dashed, the picked one solid.
- `SyncActivity.kt` — Health Connect's setup on its own, registered for `ACTION_SHOW_PERMISSIONS_RATIONALE`, so Health Connect opens it as the app's own explanation of what it reads.
- `health/HealthConnect.kt` — availability, the three permissions, and the readiness states the onboarding walks through.
- `health/ExerciseTypes.kt` — Health Connect's exercise type to HoldMyTrack's `activity_type`, normalised onto the vocabulary the other ingest paths already produce.
- `sync/SyncCandidates.kt` — what's waiting: Health Connect's sessions from the last three months the server doesn't have, and the recordings on the phone.
- `sync/SyncRunner.kt` — sends what's ticked, in batches, and reports what happened to each.
- `sync/HiddenCandidates.kt` — the rows swiped aside, per account.
- `sync/HealthConnectCard.kt` — Health Connect's setup card, on the Sync tab and in `SyncActivity`.
- `sync/ImportHistory.kt` — the history of every import, the web's `/sync` page: its latest rows on the Sync tab, and all of it on `SyncHistoryActivity.kt`, See all's screen.
- `panel/` — the map's Activities panel, the web's phone sheet: `ActivitiesPanel.kt` (the sheet and its Activities tab), `PanelState.kt` and `ActivityFacets.kt` (its rules, unit-tested), `EditActivityWindow.kt` (Edit), `TrackEditor.kt` and `EditTrackOps.kt` (its Track tab, the rules unit-tested), `StoriesTab.kt` (Stories), `SyncTab.kt` (Sync: what's on the phone, ticked and sent), `PrivateLocationEditor.kt` (a Private location, edited on the map).
- `privacy/` — the Privacy screen, from the You tab: `PrivacyActivity.kt` (the Private locations and what's kept), `CirclePreviewView.kt` (a location drawn small) and `DataExportActivity.kt` (Download your data).
- `ui/LargeText.kt` — rows that stack instead of breaking words at a large font size.

Alongside:

- `gradle/libs.versions.toml` — every dependency version, including MapLibre Native.

## Syncing from Health Connect

Three permissions, one data type. `READ_EXERCISE` and `READ_HEALTH_DATA_HISTORY` are ordinary requests; **`READ_EXERCISE_ROUTES` cannot be requested at all** and has to be granted in Health Connect → HoldMyTrack → *Additional access* → *Access exercise routes* → *Always allow*. The Sync tab says so, because that screen is two levels down and nothing links to it. Without the history permission Health Connect serves only the last 30 days — measured, not assumed.

Reading and sending are foreground-only and stop when the Sync tab leaves the screen, which is a platform constraint rather than a choice: routes written by other apps read back as `ConsentRequired` in the background whatever is granted. That is safe rather than lossy because the phone keeps no cursor: each time the tab opens, it lists what's on the phone minus what the server says it already has (`POST /v1/sync/known`), so an interrupted run leaves its unsent rows listed. Nothing is ticked by default, and only what the user ticks is sent.

## Signing in against a dev stack

There is no seeded Android account. Create one from the app itself (**Create account**), or tap **Try the demo** for the shared, read-only Demo Customer account, which needs no signup (root `docs/SPEC.md` FR-2.1). A session survives restarts, and is checked against `GET /v1/auth/me` at startup before any user layer is attached — a token revoked or expired while the app was closed would otherwise show up only as 401s in logcat, behind a map that looks merely empty.