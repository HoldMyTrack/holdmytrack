# HoldMyTrack for Android

`docs/VISION.md` §5.3: Kotlin, with Health Connect as the on-device ingest path, and the Samsung route-data limitation surfaced honestly in the UI rather than silently degrading. Rendering is MapLibre Native against the same style document the web client builds (`docs/ARCHITECTURE.md` §2.1).

- `holdmytrack/` — the app itself. Building it and pointing it at an API are covered in its own README.
- `docs/ROADMAP.md` — the phase plan, and the two platform constraints it is designed around.

Not containerised, and not planned to be: the Android SDK, the emulator and a physical device for Health Connect testing all live on the host.