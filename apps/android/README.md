# HoldMyTrack for Android

`docs/VISION.md` §4.1 (Path 2): Kotlin, with Health Connect as the on-device ingest path and the Samsung route-data limitation shown in the UI. Rendering is MapLibre Native against the same style document the web client builds (`docs/ARCHITECTURE.md` §2.1).

- `holdmytrack/` — the app itself. Building it and pointing it at an API are covered in its own README.
- `docs/` — this app's `SPEC.md`, `IMPLEMENTATION.md` and `ROADMAP.md`: what it does, how it's built, and the open work. Its shape, key decisions and stack are root `docs/ARCHITECTURE.md` §3.

Not containerised: the Android SDK, the emulator and a physical device for Health Connect testing live on the host.