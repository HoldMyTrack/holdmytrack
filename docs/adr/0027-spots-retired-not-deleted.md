# ADR-0027: A place gone from OpenStreetMap is retired, never deleted, and still shown to the accounts that captured it

## Status

Accepted; not built yet (`ROADMAP.md` Phase 1, Spots places refresh). Amends ADR-0021, whose places were only ever added and updated.

## Context

Spots' places come from OpenStreetMap through `import-spots`, run by the operator on an extract made off the server, once for the planet and then about every quarter. Until now a re-import only added and updated places: a playground torn down, a memorial moved, or a feature retagged out of the five categories stayed on the map forever. A place that's gone is worse than a missing one: someone walks to it.

Deleting it isn't free either. `spot_captures` refers to the place, and ADR-0023 made a capture something a person went out and earned; a `DELETE` would cascade it away, and the place would vanish from that person's map with no explanation. ADR-0021's visits, once built, will hang off places the same way. And `import-spots` can't tell "gone from OSM" from "not in this file": a regional extract, or a planet file cut short, looks the same as a world where most places were demolished.

## Decision

**A place the import no longer sees is retired, not deleted.** Every upsert stamps the run on the place (`last_seen_import`) and clears its `retired_at`, so a place that comes back to OSM comes back on the map. After the whole file is read, places the run didn't stamp get `retired_at` — but only when the operator says the file is the whole planet (`--planet`), and the import refuses instead, retiring nothing, if that would retire more than 1% of the live places. Nothing is ever deleted, so no capture cascades away.

**A retired place stays for those who captured it, and is gone for everyone else.** The tiles carry a `retired` flag and stay the same for every account, as ADR-0023 kept them: each map hides a retired place's badge and area unless its id is in the account's captured ids, the list it already loads for the filled badge. `GET /v1/spots` and `GET /v1/spots/{id}` return a retired place only to an account that captured it. Nobody can capture it again: `POST /v1/spots/{id}/captures` answers `410 Gone`.

**A quiet refresh costs the clients nothing.** The import counts the places it actually added, changed or brought back, and those it retired, and moves every account's tile version only when that count isn't zero.

Visits, when built, follow the same rule: a retired place keeps the visits it has and is matched against no new activity.

## Alternatives considered

- **Delete what OSM dropped.** Rejected: it takes captures with it, through `spot_captures`' foreign key, and a person's captured place disappears from their map.
- **Delete, but keep the captures by dropping the foreign key.** Rejected: a capture pointing at nothing can't be drawn, so the person still loses it from the map, only now as an orphaned row.
- **Retire after any import, regional or not.** Rejected: a US extract would retire the rest of the world. Only the operator knows what the file covers, so it's a flag rather than a guess from the file's extent.
- **No safety limit.** Rejected: a planet file cut short by a failed download, or an osmium filter that went wrong, would retire most of the map in one run. A quarter's real churn is well under 1% of places; more than that is far likelier a broken file, and the operator can look before deciding.
- **Leave retired places out of the tiles, and serve a captured account's retired places some other way.** Rejected: the tiles would need a per-account variant or a second source, against ADR-0023's one cache for everyone. A flag and the captured-ids list the maps already have do the same job.
- **Show a retired place to everyone, marked as gone.** Rejected: the map is for finding places to go, and a greyed-out badge for a demolished playground is noise to everyone who never went.

## Consequences

- `spots` grows two columns, and the table only ever grows: retired rows are kept for good. At OSM's churn that's a few percent of the table a year, not worth a cleanup that would need its own rule about captures.
- Every client must filter: a map that ignores `retired` shows places that are gone. Both maps do; a new client has to as well.
- A retired place a person captured is shown to them like any other captured place, with nothing to say it's gone from OSM.
- A regional import never retires anything, so until the planet is loaded with `--planet`, places dropped from OSM stay on the map as before.