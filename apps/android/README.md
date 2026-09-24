# HoldMyTrack for Android

`docs/VISION.md` §5.4: Kotlin, with Health Connect as the on-device ingest path, and the Samsung route-data limitation surfaced honestly in the UI rather than silently degrading. Rendering is MapLibre Native against the same style document the web client builds (`docs/ARCHITECTURE.md` §2.1).

- `fitmap/` — the app itself, currently the client shell: a full-screen map and nothing else yet. Building it and pointing it at an API are covered in its own README.
- `poc-healthconnect/` — a throwaway diagnostic that answered the roadmap's Phase 1 questions about what Health Connect really hands over, kept only until its findings are all recorded.
- `docs/ROADMAP.md` — the phase plan, the two platform constraints it is designed around, and the server-side work it depends on.

Not containerised, and not planned to be: the Android SDK, the emulator and a physical device for Health Connect testing all live on the host.