# ADR-0028: Spots have one mark, the capture; visits worked out from tracks are dropped

## Status

Accepted. Supersedes ADR-0021's visits — the five-minute dwell worked out from tracks — which were never built; ADR-0021's places stand. ADR-0023's capture (`SPEC.md` FR-15.6, `apps/android/docs/SPEC.md` FR-2.8) is now the only mark a place carries.

## Context

ADR-0021 planned to mark a place visited when an activity's points add up to five minutes inside it, worked out on the server from the user's tracks. ADR-0023 then added capture: standing in a place for 30 seconds with the Android app's capture mode on. Captures were built on the server, the web and Android; visits were not, and remained the larger job of the two — matching in the ingest job and every reprocess (track edits, Private location changes), a backfill over every history, a second badge state beside the captured one, and a second set of rules for retired places (ADR-0027).

Two marks on one badge also ask a person to learn the difference between "been there on a track" and "went there to find it", for a map whose job is to suggest where to go.

## Decision

**A place has one mark: captured.** No visits are worked out from tracks, now or later. The capture stays as ADR-0023 built it — a deliberate step at the place, kept on the server, shown on both maps.

## Alternatives considered

- **Build visits as ADR-0021 planned.** Rejected: the most work of anything left in Spots, for a second mark that overlaps the first, and it would light up places someone merely passed through on a long ride as if they'd sought them out.
- **Make a capture count as a visit, or a visit as a capture.** Rejected for the reasons ADR-0023 gives: 30 seconds isn't five minutes, and either merge blurs what the mark means.
- **Keep visits on the roadmap, unscheduled.** Rejected: a plan nobody intends to build still shapes other designs — ADR-0027 already had to write a rule for it.

## Consequences

- The back catalogue doesn't light up: a place is marked only once someone goes there with the Android app and captures it. The web can't capture (ADR-0023), so a person without the Android app never marks a place.
- No `spot_visits` table, no matching in ingest or reprocessing, no backfill job, and no per-account flag in the spots tiles, which stay the same for every account.
- Private locations don't touch Spots at all: a capture is made live by the phone, and nothing about places is derived from the clipped tracks.