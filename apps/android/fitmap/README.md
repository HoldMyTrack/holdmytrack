# fitmap-android

The FitMap Android app (`apps/android/docs/ROADMAP.md`). Today it is the client shell: a full-screen MapLibre Native map rendering the style document the API serves at `GET /v1/map/style/{flavor}`, with no account, no user layers and no Health Connect yet — those are the roadmap items that follow.

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

- `app/src/main/kotlin/dev/fitmap/android/MainActivity.kt` — the map, its style URL, and MapLibre's lifecycle forwarding.
- `gradle/libs.versions.toml` — every dependency version, including MapLibre Native.
- `../poc-healthconnect/` — a separate, throwaway build answering the roadmap's Phase 1 questions. Not a module of this project, and deleted once its findings are recorded.