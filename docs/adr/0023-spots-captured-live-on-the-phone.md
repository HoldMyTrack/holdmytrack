# ADR-0023: A spot can be captured live in the Android app, by staying 30 seconds inside it, and the capture is kept on the server

## Status

Accepted. Built: the server and the web show captures (`SPEC.md` FR-15.6, `IMPLEMENTATION.md` §4.25), and the Android app captures (`apps/android/docs/SPEC.md` FR-2.8). Amends ADR-0021. ADR-0028 later dropped ADR-0021's visits, so the capture is the only mark a place carries and this ADR's lines about visits no longer apply.

## Context

ADR-0021 made Spots a map of places worth going to, and decided a visit is worked out from tracks, with no prompt and no live tracking. That serves the back catalogue well, and it says nothing about the moment someone is actually going to a place: they picked a playground or a viewpoint on the map, and the app does nothing to get them there or to mark the arrival. The request was a small game on top of Spots: choose a spot, head for it, stand in it for a moment, and see it marked on the map from then on. Four choices decide how it behaves: which spot counts, how long, where a capture is kept, and how the maps show it.

## Decision

**Capture is a mode in the Android app, started from a spot's popup, aimed at that one spot.** While it's on, the app reads the phone's GPS — only while it's open, the same foreground-only access as "Find my location" — and shows the distance and an arrow to the spot, and the spot areas are drawn darker. Inside the spot's area it counts up; 30 seconds inside, on fixes accurate to 25 m, captures the spot. Stepping outside starts the count again. It can be switched off at any time, and ends by itself once the spot is captured.

**A capture is kept on the server, one per account and place** (`spot_captures`), sent with the position the phone last measured, which the server checks against the place's area within 10 m. The first capture of a place is the one kept.

**Both maps show captured spots with a filled badge** — the accent behind a white icon — from the account's list of captures, not from the tiles. Only the Android app captures; the web shows.

**A capture is not a visit.** Visits stay as ADR-0021 decided — five minutes on a track, worked out on the server — and aren't built yet. A capture is a separate, deliberate mark.

## Alternatives considered

- **Keep captures on the phone only.** Rejected: no server work, but a reinstall or a new phone loses them, and the web couldn't show them.
- **A capture counts as a visit.** Rejected: 30 seconds isn't ADR-0021's five minutes, and making it so would either loosen visits or turn capture into a five-minute wait; and it would make building visits a prerequisite.
- **A per-account `captured` flag in the spots tiles.** Rejected: the spots tiles are the same for every account today; a flag makes them per-account and makes every capture bump the tile version, dropping the account's cached track and fog tiles too. An account's captures are few, and a style expression over their ids does the same job.
- **Capture any spot you walk into, guided to the nearest.** Rejected in favour of the one spot picked: the popup is where the choice is made, and a single target keeps the arrow and distance unambiguous.
- **Five minutes inside, as for visits.** Rejected: a five-minute wait with the app open is a chore, not a game; 30 seconds on accurate fixes is enough that walking past doesn't capture.
- **A background geofence.** Rejected for the reason ADR-0021 gives: "Allow all the time" location access, which the apps deliberately don't ask for.

## Consequences

- `VISION.md` §1.1's "never from live tracking" no longer holds for Spots as a whole: capture is a live step, though only with the app open and only while the user has switched it on.
- The phone needs a place's whole area, which the tiles only carry clipped per tile, so there's an endpoint for one place's detail.
- Capture needs the Android app. The web can't capture: a desktop browser's position is too coarse, and a phone's browser can't be relied on to keep reading it.
- The captured mark and, later, the visited mark are two different marks on the same badge; when visits are built, the badge will need to show both.