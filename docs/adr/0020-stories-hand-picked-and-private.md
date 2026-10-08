# ADR-0020: Stories are hand-picked, private sets of activities, shown in Normal mode with a story-scoped date picker

## Status

Accepted. ADR-0036 adds sending a copy of a Story to another account; the Story itself stays private.

## Context

A trip — a multi-day hike, a holiday, a race weekend — is several activities over several days, often with a drive at each end. Once it's over it dissolves into the rest of the history: the date-range picker can find its days, but not tell the trip's activities apart from everything else on them, and nothing keeps its name, its description or its totals. Stories are meant to fix that: a named set of activities that can be opened on the map on its own, with joint statistics, and that one activity can belong to several of (the hike, and also "Summer 2026"). The point of a Story is to remember a trip (`VISION.md` §4.2). Four choices decide what that means in practice: how an activity gets into a Story, who can see one, which map modes a Story applies to, and what the date-range picker does while one is open.

## Decision

**Membership is hand-picked.** A Story holds exactly the activities the user put in it: from the Activities panel's "Create story" over the checked activities, from the Edit window's Stories tab, or by removing one from the Story panel. A new upload from the trip's dates doesn't join on its own. Deleting an activity removes it from every Story it was in; a Story left with none stays until it is deleted itself, and deleting a Story never deletes an activity.

**Stories are private.** Only the owner sees them, like everything else in an account. A Story is shared the way the rest of the map is: as an exported image, taken while it's open (`IMPLEMENTATION.md` §4.3.3).

**A Story view is Normal mode only.** It narrows the drawn tracks, the Activities panel's list and the date-range picker to the Story's activities. Fog of War and Heatmap stay all-time and ignore it.

**The date-range picker keeps working inside a Story, over the Story's activities only.** Its span runs from the Story's first activity day to its last and its bars count only the Story's activities; it opens with the whole Story selected, and can be narrowed down to a single day as it can outside one (`SPEC.md` FR-6). The Story's joint statistics describe the whole Story regardless of the selection; the list, the panel's running totals and the drawn tracks follow it.

## Alternatives considered

- **A Story as a saved query** (a date span plus activity types) that fills itself. Rejected: only the person who went knows that Tuesday's drive was part of the trip and Wednesday's was the commute, and a query can't express "not that one" without turning into a hand-picked list with extra steps. It also can't hold one activity in two unrelated Stories without both queries matching it by accident.
- **Hand-picked, plus an optional date span that offers "add the N activities from these dates".** Not rejected on principle, just not needed for the first version: a date-range selection plus select-all and "Create story" already does the same in two clicks.
- **An unlisted link that opens a Story for anyone who has it.** Rejected for now: a Story is a precise record of where someone was and when, and a shareable one needs signed, revocable links, Private locations enforced on the shared view, and a decision about everything a social feature carries (`VISION.md` §5.7, §5.8). The exported image covers sharing without any of that.
- **Fog of War and Heatmap per Story.** Rejected: both are precomputed per-account raster pyramids (ADR-0003); a pyramid per Story is a storage cost that grows with every Story made and has to be rebuilt on every membership change (`VISION.md` §4.3), for a view whose tracks already show exactly where the trip went.
- **Hiding the date-range picker while a Story is open**, showing all of its activities at once. Rejected: it takes away browsing a trip day by day, and it puts no bound on how many tracks a large Story draws at once.

## Consequences

- One new pair of tables (a Story, and its activity memberships, cascading from both sides), and one new filter — a Story id — on the activity list, the tracks tiles and the histogram, next to the date and type filters they already take. The map needs no second rendering path.
- A membership change alters what a Story's tracks tiles contain, so it bumps the account's tile version like any other change to what the map draws.
- A Story's contents can go stale in one direction: an activity uploaded later from the trip's dates isn't in it until the user adds it.
- Sharing a Story is an image, not a place other people can visit. Anything more is a later, deliberate decision.