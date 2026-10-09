# ADR-0039: The app is the gate: every source brings in only what the user ticks, and nothing is guessed on their behalf

## Status

Accepted. Built so far: the Android app's list of what's on the phone to tick and send (`SPEC.md` FR-3.6, FR-3.8), and Google Maps Timeline import removed. The rest is in `ROADMAP.md`'s "The app is the gate". Supersedes ADR-0011, since Takeout import goes, and the archive parts of ADR-0001 and ADR-0032.

## Context

Every source poured everything in. Health Connect sync and in-app recording sent everything since the last sync, a `.zip` or a Google Takeout export brought every activity it held (up to 5,000 files), and the Google Maps Timeline import let the user choose only by date range and mode. So the Activities panel filled with dog walks, commutes and grocery runs. They cost storage and add nothing to a product about journeys and exploring: three months of a real Timeline came to 584 activities (358 drives and 218 walks) and about 60 MB, against `VISION.md` §4.3's assumption of about 27 MB per user in all.

Two pieces of the pipeline guessed on the user's behalf:

- **Timeline endpoint trimming.** A trip's start or end place more than 1.5 km from its route and too fast to reach was dropped (46 of one account's 536 trips). Right most of the time, but the user couldn't see which trips it changed or why.
- **Automatic cross-source duplicate handling.** A new activity overlapping a live one by 80% of the longer one's time was matched to it, and the poorer copy was marked `superseded_by` the richer one and hidden (`IMPLEMENTATION.md` §4.6). It got splits wrong (`KNOWN_ISSUES.md`), it decided which copy was "richer" by a rule, and the hidden copies still had to be filtered out of every read: tiles, Fog and Heatmap, export, Stories and the rest.

Both exist to clean up after an import the user didn't choose activity by activity. Once the user chooses, they aren't needed.

## Decision

- **Every source is a gate, and nothing is ticked by default.** Each source shows what it would bring in, and the user ticks what to keep: the Android app's list of Health Connect sessions and waiting recordings, and future cloud connectors. What isn't ticked never reaches the server. Nothing starts ticked, in any source.
- **Fog of War shows only what's kept.** It is drawn from the account's activities and nothing else: no "fog-only" contributions from history that wasn't imported. Deleting an activity takes its fog away, so the fog stays reversible.
- **Stories are the main feature; the Activities list stays the everyday view.** The last few days are what people look at most, so navigation and the map's opening view don't change.
- **No guessing.** Automatic duplicate handling goes, and so does `superseded_by`. In its place is a hint: a candidate whose time overlaps a live activity is marked "overlaps *Morning walk*" in the list it's picked from, and stays unticked like everything else. Ticking it anyway makes a second activity. The user fixes a track with the edit tools. Idempotency stays (the content hash and `external_id`), since that's what makes a re-sync or a re-import safe.
- **`.zip` and Google Takeout import are removed.** They bring in thousands of activities at once, the opposite of a gate. Getting the export is hard for a regular user, and a large one fills the single worker queue for everyone. Bulk history comes later through cloud connectors, with a picker. Path 3 still needs no one's permission (ADR-0001), through uploading single files, up to 20 at once.
- **Google Maps Timeline import is removed.** Its window picked by date range and mode, never a single trip, so keeping one drive meant keeping every drive in the range: a bulk import with a filter. A Timeline is mostly the routine, its tracks are a point every few minutes with no elevation, and its reader (an undocumented format, twice: in the browser and on the phone) is where endpoint trimming lived. Its trimming goes with it. The server refuses the `timeline` sync source; activities already imported stay as they are.

## Alternatives considered

- **Fog-only contributions.** Bring in everything for the fog and keep only the ticked activities. The fog would then show places the user can't see, select or delete, and it couldn't be undone activity by activity. It would also keep the storage cost of everything, since fog is per-activity masks (`IMPLEMENTATION.md` §4.2.3).
- **Smart default ticking.** Tick what looks like a journey (long, far from home, unusual) and leave the routine unticked. Another heuristic, wrong in both directions, and the user would have to check every row anyway.
- **Keep automatic duplicate handling (auto-supersede).** Saves a tap when the same walk comes from two sources. With a gate, the user sees both and picks one, and the hint tells them when they're about to make a second copy. The automatic version costs a filter in every read and gets splits wrong.
- **Keep Timeline, with a picker per trip.** Every trip listed and drawn, each ticked on its own. Real work for the coarsest tracks HoldMyTrack takes in, and a season of commutes is hundreds of rows to pick a holiday's drives out of; recording the trip in the app gives a better track.
- **Keep archives, with a preview and select step.** A two-step import on the server: parse, show, then ingest what's picked. It would hold uploads of up to 2 GB between the steps and send a preview of thousands of routes. Connectors with a picker cover the same need without the export step.

## Consequences

- An account holds what its owner chose. The Activities list, Fog of War and totals are smaller, and per-account storage stays near `VISION.md` §4.3's assumption.
- More taps: every sync and import starts with nothing ticked. That's the point, but a first sync of three months of walks takes a Select all.
- Travellers whose only record of a trip is Google Maps Timeline have no way to bring it in (`VISION.md` §3.1).
- Someone with a large archive (a Strava export, a Takeout) can't bring it in at once. They upload up to 20 files at a time or wait for a connector. Path 3 is weaker for them than it was.
- The same walk from two sources can now be kept twice, if the user ticks both despite the hint. Both count in totals and Fog of War until one is deleted.
- Removing `superseded_by` takes a migration that deletes the superseded copies already stored, and a change to every read that filters them out.
- Accounts that already hold routine activities clean them up with the existing Delete group (FR-5.13) over a type and date filter.