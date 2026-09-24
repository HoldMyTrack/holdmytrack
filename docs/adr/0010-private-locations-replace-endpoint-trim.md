# ADR-0010: Private locations replace the fixed endpoint trim

## Status

Accepted. Refines, but does not supersede, [ADR-0002](0002-privacy-applied-at-ingest.md): privacy is still applied at ingest; what changes is *which* points it removes.

## Context

Every activity used to lose a fixed radius from both ends at ingest (`users.privacy_trim_cm`, 100 m by default, adjustable 0–200 m in Settings) — the "automatic endpoint trimming, opt-out" `VISION.md` §7 asked for, on the theory that a track's endpoints are where the user lives.

That theory is wrong often enough to matter. On a multi-day trail, each day ends at a campsite or trailhead and the next day starts there. None of those endpoints is sensitive, yet the trim cut ~200 m out of the trail at every day boundary, so the fog showed a string of disconnected pieces where a continuous trail should have been. The trim also took ~200 m off every activity's distance, and it didn't reliably hide home either: hundreds of tracks each cut 100 m from the same door leave an empty ring in the fog with the door in its middle.

The accounts are private. The trim protected nothing from anyone except the owner — and only mattered at all for what leaves the account (an exported image, a screenshot, a future shared map). The places worth hiding are the few a user can name: home, work, a relative's house.

## Decision

- **Remove the fixed endpoint trim entirely** — the column, the Settings field, the API field, the ingest step.
- **Private locations** (the long-planned `privacy_zones`, `IMPLEMENTATION.md` §3.7) become the only privacy clipping: circles the user places on the map, 50–2000 m. At ingest, a track's leading and trailing points inside any of them are dropped and each end moved onto the circle's edge. Everything outside every circle keeps its full recorded geometry.
- **Changes are retroactive**: each create/move/resize/delete reprocesses the affected activities from their raw payloads (`reprivacy` job), marking them Pending in the Activities panel until done.
- **Ends only, for now.** A track passing *through* a circle mid-way is left whole; splitting it needs a multi-part trajectory and is a ROADMAP item.
- **Stats follow the visible part**, as they did under the trim.
- **No backfill.** Activities already ingested keep the trim they were processed with until something reprocesses them (an Edit track, or a Private location change that includes them).

## Alternatives considered

- **Keep the trim, add a per-activity "don't trim" override.** Fixes the trail once the user notices and toggles every day of it; keeps the cost as the default everywhere else. Rejected as treating the symptom.
- **Trim only endpoints that recur** — cluster start/end points and trim only near clusters hit several times (home), leaving one-off campsites alone. Closer to the real intent, but it's an automatic guess about what a user considers private, it still leaves the ring-around-home tell, and it's more machinery than letting the user say so. Could still come back as *suggested* Private locations.
- **Skip the trim where consecutive activities join up.** Fixes the trail, but a normal out-and-back from home also "joins up" at home — it would un-hide exactly the place the trim was for.
- **Keep the stored track whole and apply privacy only on the way out** (export, sharing). Gives the owner an undistorted view, but contradicts ADR-0002 — every output path would have to re-implement the exclusion — and leaves screenshots of the owner's own map, the product's main marketing channel, unprotected.
- **Backfill on deploy** (reprocess every existing activity with no trim). Rejected by choice: existing accounts had no Private locations yet, so every home would reappear at once, with no consent to that change.

## Consequences

- Multi-day trails, and any activity that doesn't start or end somewhere private, fog continuously and report their real distance.
- Protection is opt-in: an account with no Private locations hides nothing. The Settings page and the account menu point to the map window, and the demo account ships with one, to make that visible.
- A track passing through a circle still shows inside it until splitting lands.
- A circle change can reprocess hundreds of activities in one job; the fog/heatmap render runs once per batch to keep that affordable.
- Pre-change activities keep their trim indefinitely unless reprocessed — accepted, and documented in `SPEC.md` FR-8.1.
- Retroactive clipping is bounded by raw-payload retention (`IMPLEMENTATION.md` §5.7), as ADR-0002 already noted.