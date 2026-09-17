# ADR-0007: In-app GPS recording submits directly, not via a Health Connect/HealthKit round-trip

## Status

Accepted. Planned, not yet built — tracked in `apps/android/docs/ROADMAP.md`.

## Context

`VISION.md` §1.1 previously stated flatly that FitMap does not record activities: no start button, no live GPS, no auto-pause — the product begins where a watch's recording ends. That line is now revised (`VISION.md` §1.1, §4.1): the mobile app will add a deliberately narrow, GPS-only recording capability, aimed at casual activities someone would not otherwise bother tracking at all (a road trip, a dog walk, a forest walk) — not at replacing a dedicated fitness tracker for a real workout.

Once the app is willing to capture location itself, there are two structurally different ways the resulting track can reach the server, and the choice has to be made once, deliberately, because it shapes the permissions the app requests, the code paths it needs, and how the feature interacts with Path 2's existing Health Connect sync (`docs/adr/0001-three-independent-ingest-paths.md`, `apps/android/docs/ARCHITECTURE.md`).

## Decision

**Submit a finished recording directly to the existing `POST /v1/sync/activities` endpoint, with a new `source` value, immediately when recording stops.** No intermediate write into Health Connect, no read-back through Path 2's sync machinery.

Concretely: the app buffers location updates locally while a recording is in progress (in memory or a small on-device store — an implementation detail for `apps/android/docs/ROADMAP.md`, not this ADR), and on "Stop," normalizes and posts the finished track through the same wire shape and endpoint `docs/IMPLEMENTATION.md` §4.0.3 already defines for Path 2, under its own `source` value distinct from `"healthconnect"` and `"healthkit"` — the activity was authored by FitMap itself, not read from the platform health store, and the schema's `source` column (`docs/IMPLEMENTATION.md` §3.3) already exists precisely to keep that distinction visible.

## Alternatives considered

- **Write the finished recording into Health Connect first, then let Path 2's existing sync flow read it back.** Rejected. This would require the app to declare `WRITE_EXERCISE_ROUTE` (a permission it does not otherwise need), and — worse — it would drag the *reading it back* half of Path 2's already-fought platform constraints into a case where they buy nothing: `READ_EXERCISE_ROUTES`'s foreground-only behavior and `ConsentRequired` on background reads (`apps/android/docs/ARCHITECTURE.md` §1.1, `docs/IMPLEMENTATION.md` §4.0) exist because Health Connect cannot assume a route written by *another app* is safe to hand back in the background. FitMap already has the just-recorded points in memory, in the same process, the moment recording stops — routing them through a platform intermediary only to read them straight back is complexity with no corresponding benefit here.
- **A brand-new, recording-specific endpoint.** Rejected. `POST /v1/sync/activities` already accepts a normalized `{external_id, activity_type, points}` batch and is already idempotent and bounded exactly the way a client-originated batch needs to be (`docs/IMPLEMENTATION.md` §4.0.3) — a recorded activity is not a different shape of data, just a different `source` and a different (client-generated, e.g. a UUID minted at recording start) `external_id`.

## Consequences

- **No new server-side ingest path.** This reuses Path 2's endpoint, its idempotency rule (`(user_id, source, external_id)`), and its downstream pipeline unchanged (`docs/IMPLEMENTATION.md` §4.0–§4.1) — the only server-side change is allowlisting the new `source` value.
- **The recording feature does not inherit Path 2's foreground-only constraint for the reason Path 2 has it**, since there is no other app's data to be denied access to — but a recording *in progress* is still realistically a foreground-service-bound activity for its own reasons (the OS is free to kill a plain background process, and continuous GPS is a battery cost users need visible feedback about), which `apps/android/docs/ROADMAP.md`'s own phase for this feature has to design for on its own terms, not by inheriting Path 2's.
- **Cross-source deduplication (`docs/IMPLEMENTATION.md` §4.6) now has a fourth `source` value to reason about.** The existing fuzzy-window match (same user, same type, start within ~30s, distance within ~1%) needs no change in mechanism, but a recorded activity and, say, a watch's own recording of the same outing are now a realistic collision case worth keeping in mind as this ships.
- **A recorded activity never touches Health Connect at all**, by design — it is not visible to other apps on the device the way a Health Connect–backed recording would be. Accepted as the right tradeoff for a feature scoped to "get this one walk onto the map," not "be a system-of-record other apps can read from."
