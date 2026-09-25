# ADR-0011: The Takeout reader is Go code in the server, not a pathify subprocess

## Status

Accepted. Reverses the "shell out to pathify" choice recorded in `IMPLEMENTATION.md` §4.0.2 when Takeout import was first built; that choice had no ADR of its own.

## Context

A Google Health / Fitbit Takeout export keeps GPS fixes (`gps_location_YYYY-MM-DD.csv`, one file per UTC day, every recording device interleaved) apart from the activity logs (`exercise-N.json`) that say which stretch of a day was a walk. Importing it is a join. Takeout import first shipped by running `pathify` (github.com/np25071984/pathify, a Rust CLI by the same author) as a subprocess, on the theory that reimplementing its join in Go would duplicate a lot of already-solved work.

In practice that cost more than it saved:

- The server image compiled pathify from source (`cargo install`), pulling the ~1.5 GB Rust toolchain image into every uncached build. It was the slowest step in the image and helped fill the sandbox VPS's disk mid-build.
- The runtime image had to be `distroless/cc` rather than `distroless/static`, only because pathify needs libc.
- The handler wrote the upload to a temp file, ran pathify twice per activity type, and read GPX files back out of temp directories, although the zip was already parsed in memory.

The duplication turned out smaller than it looked. Most of pathify's Takeout code is CLI-facing (menus, `--type` matching, report text) or general-purpose (segment merging), and whole-activity dedupe across sources already exists in `internal/ingest`. What HoldMyTrack actually needs is about 400 lines: read the two file families, detect the logs' wall-clock offset, slice each log's window out of the day files, keep one device's fixes, and write a GPX.

## Decision

- **Port the reader to Go** as `internal/takeout`, reading the `*zip.Reader` the upload handler already holds. No temp files, no subprocess.
- **Write the same GPX pathify wrote, byte for byte**: same file names, same `<metadata>`/`<trk>` shape, same float digits (including Rust's round-half-up on exact float ties, where Go rounds half to even), same XML escaping, same 120-second segment gap. An activity's `external_id` is the SHA-256 of its raw payload, so byte-identical output means re-importing an export that was imported before the port is still skipped as `already_processed`.
- **Keep extracting one activity type at a time**, as the handler always did, because the wall-clock offset is detected from the activities being extracted and a different selection could land on a different offset.
- **Held to pathify's own output by test**: `testdata/` holds pathify 1.4.0's output for its synthetic fixture, and an env-gated test compares against a real export's pathify output, which can't be committed. At the port, all 118 activities of the one real export available matched byte for byte.
- The server image drops the Rust stage and moves to `distroless/static-debian12`.

## Alternatives considered

- **Keep pathify and cache the Rust build layer in CI.** Helps CI only; the VPS still builds the toolchain from scratch, and the image stays on `distroless/cc`.
- **Ship prebuilt pathify binaries from its own repo and download them in the Dockerfile.** Removes the toolchain from the build and keeps one implementation, but keeps the subprocess, temp files and libc dependency, and adds a release pipeline to maintain in another repository. Reasonable, but it keeps most of what was awkward about the integration.
- **Port without byte-identical output.** Simpler float formatting and free choice of GPX layout, but a re-import of an already-imported export would no longer be skipped up front by the exact `external_id` check: every activity would be stored and processed again, relying on the fuzzy start-time-and-distance dedupe (`internal/ingest/dedupe.go`) to catch each copy. And the port would have nothing exact to be tested against.

## Consequences

- Server builds need no Rust toolchain; the runtime image is a static binary on `distroless/static`, and the server runs no external binaries at all.
- Two implementations of the join now exist, pathify's and this one. They're pinned together only by the byte-for-byte test against pathify 1.4.0's output: a later pathify fix doesn't reach HoldMyTrack unless it's ported too, and a change here that alters the GPX bytes changes the content hash, so re-imports of already-imported exports stop being skipped by the exact-ID check and fall through to the fuzzy dedupe. Changing the GPX format is therefore a deliberate decision, not a refactor.
- The real-export comparison can only be rerun by someone who has a real export and a pathify binary; the committed fixture test is what CI runs.
- Takeout activities that claim GPS but have no fixes in their window are now reported per type as `no GPS points found`, where the subprocess's non-zero exit used to surface as `extraction failed`.