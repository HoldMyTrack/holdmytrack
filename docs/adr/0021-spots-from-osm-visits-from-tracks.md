# ADR-0021: Spots are bulk-imported from OpenStreetMap, and a visit is five minutes inside one, worked out from tracks

## Status

Accepted. The places are built on the web (`SPEC.md` FR-15, `IMPLEMENTATION.md` §4.25), with a 30 m circle rather than 50 m for a place mapped as a point; visits are not built yet (`ROADMAP.md` Phase 1). Android follows (`apps/android/docs/ROADMAP.md`). ADR-0023 adds capturing a spot live in the Android app, a mark separate from a visit, and so no longer rejects an in-app mechanic outright. Refreshing the places — a manual quarterly re-import that retires places gone from OSM rather than deleting them — is planned (`ROADMAP.md` "Spots places refresh").

## Context

Spots is a map of outdoor places worth going to — a playground or a dog park nearby, a monument, a viewpoint, a historic site — and of which of them the user has already been to (`VISION.md` §1.1, §4.2). It's HoldMyTrack's first feature that looks forward rather than back: its job is to help decide where to go next. Four choices decide what it costs and how it behaves: where the places come from, how a visit is detected, how places appear on the map, and which kinds of places there are.

## Decision

**The places are a one-time bulk import from OpenStreetMap.** An operator runs an import from an extract filtered to the categories below; there is no admin page for picking places, no user suggestions, and no photos. Refreshing the data is later work.

**Five categories, each with a clean OSM tag:** Playground (`leisure=playground`), Dog park (`leisure=dog_park`), Monument (`historic=monument`, `historic=memorial`), Mesmerizing view (`tourism=viewpoint`) and History (`historic=castle`, `ruins`, `fort`, `archaeological_site`).

**A visit is worked out from the user's tracks: an activity visits a spot when its points add up to at least five minutes inside the spot's area.** The area is the OSM outline, or a 50 m circle around a place mapped as a single point. Matching runs whenever an activity is processed or reprocessed, on points already clipped by Private locations, and once over every existing history after the import. Visits are automatic; there is no prompt and no un-mark.

**Spots are one map layer behind one "Show POI" toggle, the same in Normal, Fog of War and Heatmap**, drawn from zoom 14 up: a category icon for a place not yet visited, a filled one for a visited place. Clicking one offers Copy address (OSM's address tags, or its coordinates) and Navigate (an external navigator at its coordinates). Spots don't change the fog.

## Alternatives considered

- **Admin-curated places, picked from an OSM search on an admin page, plus user suggestions into a review queue.** Rejected: better places, but the planet only gets covered one admin click at a time, and a suggestion queue is moderation — staff time, which a one-maintainer, donation-funded project has least of (`VISION.md` §6.3, §5.7).
- **A prompt when the phone is near a spot ("Are you at X?"), with the app open.** Rejected: it needs the app open at the right moment and misses every activity recorded on a watch.
- **A background geofence that wakes the app near a spot.** Rejected: it needs "Allow all the time" location access and a Play Store background-location review, and it breaks the apps' deliberate no-background-location design.
- **Capturing a visit by starting the in-app GPS recording at the place.** Not a separate mechanism: a recording is one more track, and track matching already counts it.
- **Any track that touches a spot counts.** Rejected: driving past a monument or running along a park's edge would collect it, and the visited marks would stop meaning anything.
- **Revealing a visited spot's whole area in the fog.** Rejected: it makes a second coverage source for the fog pipeline, and it makes the fog say someone was somewhere their track never went.
- **Architecture, and any `historic=*`.** Rejected: OSM has no clean tag for architecture worth seeing, and all of `historic=*` is millions of boundary stones, wayside crosses and plaques.

## Consequences

- Places exist wherever OSM has them, from day one, and cost no curation. Their quality is OSM's: a school's private playground or a misplaced viewpoint shows up as mapped.
- One fixed-size table of several million places (roughly 1 GB with its spatial index) and one table of visits, which cascades away with an activity or a place. Matching is an indexed intersection per activity plus a dwell sum over the points that fell inside.
- The import has to run off-box: filtering the planet file needs more than the 1 vCPU / 2 GB server has (`ROADMAP.md`).
- Visits need timestamped points: a track with none can't visit anything. A spot inside a Private location is never visited, since the points there are clipped before matching.
- Spots tiles carry a per-user visited flag, so they're per-account tiles like the track tiles, cached by the same tile version.
- Many OSM places have no address; Copy address then copies coordinates, which every navigator accepts. Reverse-geocoding millions of places isn't an option under Nominatim's usage policy.