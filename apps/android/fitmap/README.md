# fitmap-android

The FitMap Android app (`apps/android/docs/ROADMAP.md`). It renders the map, holds a session, and syncs exercise sessions in from Health Connect: a full-screen MapLibre Native map drawing the style document the API serves at `GET /v1/map/style/{flavor}`, sign-in against the same accounts the web client uses, the Normal / Fog of War / Heatmap toggle over the user's own layers, and Path 2 on-device ingest into `POST /v1/sync/activities`.

The basemap draws whether or not anyone is signed in — the style endpoint is unauthenticated and the archive it points at is a plain `pmtiles://` URL that MapLibre Native reads natively — so a signed-out FitMap is a working map rather than a login wall. Signing in is what adds the three user layers, every one of which is behind `requireAuth` server-side.

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

`BuildConfig.API_BASE_URL` is baked in at build time from the `fitmap.apiBaseUrl` Gradle property, which `gradle.properties` defaults to `http://10.0.2.2:8080` — the emulator's alias for the host's loopback, so it reaches a `make up` stack on the host. Override it for anything else:

```sh
# Physical device over USB, against a host stack whose API is published on 8081.
# The style document's own asset URLs resolve against BASEMAP_ORIGIN, which defaults to
# APP_BASE_URL (the web dev server on 5173) — so the basemap needs its port forwarded too.
adb reverse tcp:8081 tcp:8081
adb reverse tcp:5173 tcp:5173
./gradlew installDebug -Pfitmap.apiBaseUrl=http://127.0.0.1:8081
```

Cleartext `http://` is permitted in debug builds only (`app/src/debug/AndroidManifest.xml`), so a release build cannot quietly ship pointing at one.

## Layout

Everything is under `app/src/main/kotlin/dev/fitmap/android/`:

- `FitMapApplication.kt` — process-level setup. The load-bearing line is `HttpRequestUtil.setOkHttpClient`, which replaces MapLibre Native's own HTTP client with the app's. The map SDK fetches the style, the archive and every tile through a stack the app's API client never sees, so without this the session would reach none of the user layers.
- `MainActivity.kt` — the map, the on-map mode toggle, the burger menu (Profile, Sync), and MapLibre's lifecycle forwarding.
- `ProfileActivity.kt` — the account half of the burger menu: who is signed in, and sign in/out.
- `SignInActivity.kt` — sign in, create an account, or start a demo account.
- `net/Session.kt` — the session token, held process-wide and mirrored to private `SharedPreferences`.
- `net/FitMapApi.kt` — the whole HTTP surface: the shared `OkHttpClient`, the interceptor that attaches the token to FitMap's own origin and nowhere else, and the five calls the app makes.
- `map/MapOverlays.kt` — the tracks, fog and heatmap layers, their ordering beneath the basemap's labels, and the three-way mode toggle.
- `SyncActivity.kt` — Health Connect onboarding and the sync run. Also registered for `ACTION_SHOW_PERMISSIONS_RATIONALE`, so Health Connect opens it as the app's own explanation of what it reads.
- `health/HealthConnect.kt` — availability, the three permissions, and the readiness states the onboarding walks through.
- `health/ExerciseTypes.kt` — Health Connect's exercise type to FitMap's `activity_type`, normalised onto the vocabulary the other ingest paths already produce.
- `sync/SyncCursor.kt` — the watermark. Read its comment before changing anything about it.
- `sync/SyncRunner.kt` — one foreground sync run: read, classify, batch, post, advance.
- `SyncStatusActivity.kt` — sync history, pending work, failure detail, and the activities cross-source deduplication took out of circulation because they were already here from another source.

Alongside:

- `gradle/libs.versions.toml` — every dependency version, including MapLibre Native.
- `../poc-healthconnect/` — a separate, throwaway build answering the roadmap's Phase 1 questions. Not a module of this project, and deleted once its findings are recorded.

## Syncing from Health Connect

Three permissions, one data type. `READ_EXERCISE` and `READ_HEALTH_DATA_HISTORY` are ordinary requests; **`READ_EXERCISE_ROUTES` cannot be requested at all** and has to be granted in Health Connect → FitMap → *Additional access* → *Access exercise routes* → *Always allow*. The sync screen says so, because that screen is two levels down and nothing links to it. Without the history permission Health Connect serves only the last 30 days — measured, not assumed.

The run is foreground-only and stops when the screen does, which is a platform constraint rather than a choice: routes written by other apps read back as `ConsentRequired` in the background whatever is granted. That is safe rather than lossy because the watermark only moves over activities the server has already confirmed, so an interrupted run is simply repeated. **If you change one thing in `sync/`, read `SyncCursor`'s comment first** — advancing it past a record whose route was not read produces a history that is complete except for the map, which is worse than no sync at all.

## Signing in against a dev stack

There is no seeded Android account. Create one from the app itself (**Create account**), or tap **Try the demo** for an ephemeral account that needs no signup and is purged after a day. A session survives restarts, and is checked against `GET /v1/auth/me` at startup before any user layer is attached — a token revoked or expired while the app was closed would otherwise show up only as 401s in logcat, behind a map that looks merely empty.