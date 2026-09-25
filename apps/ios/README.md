# HoldMyTrack for iOS — not implemented

Phase 2 (`docs/VISION.md` §5.3): Swift, with HealthKit and Apple Watch as the on-device ingest path — the stronger of the two mobile routes, and the second of the pair to be built. Android goes first so the Path 2 sync contract is designed against the harder platform; iOS inherits it. Rendering is MapLibre Native against the same style document the web client builds (`docs/ARCHITECTURE.md` §2.1).

Not containerised, and not planned to be: Xcode, the simulator and a physical device for HealthKit testing all live on the host.