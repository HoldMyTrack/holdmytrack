# ADR-0035: An activity's times show, and its days group, in the timezone it was recorded in

## Status

Accepted. Built in `internal/geo` (`seed-timezones`), ingest, the activity queries and both clients (`IMPLEMENTATION.md` §3.3, §3.24, §4.30).

## Context

The server grouped activities into days by the account's own timezone (`users.timezone`), while the web and Android app formatted times in the device's timezone. An account on New York time viewed from a browser in Berlin showed a 21:30 run in the date slider's 9 March slot, but labelled it "10 Mar, 03:30". Making every time use the account's zone would fix that mismatch, but would still be wrong for a trip: a 07:00 run in Tokyo would read 18:00 the day before, which isn't how anyone remembers it. HoldMyTrack is about the places someone has been, so the wall clock where they were is the time that matters.

## Decision

**Each activity stores the IANA name of the zone it was recorded in, and every time and every day, week or month an activity is shown or grouped in uses that zone.**

- **The zone's name, never an offset or a stored local time.** Offsets change: Moscow kept summer time until 2011, sat on UTC+4 until late 2014 and has been on UTC+3 since. The tz database keeps each zone's whole history, so `started_at AT TIME ZONE timezone` gives the offset in force on the activity's own date, and an updated tz database corrects every activity at once.
- **Looked up from the first recorded point**, before Private locations clip it, in timezone-boundary-builder's polygons loaded into PostGIS (`tz_parts`). Where no polygon covers the point (before the polygons are seeded, or for a zone the database's tz data doesn't know yet), the activity takes the account's zone.
- **The full release with ocean zones** (`timezones-with-oceans`): the ocean zones (`Etc/GMT±N`) give a point at sea a zone; the full set rather than the `-now` variant, which merges zones whose clocks agree only today and would give an older activity the wrong offset.
- **Seeded like the country outlines** (ADR-0034): the release zip goes into the app bucket by hand, and `seed-timezones` loads it and re-matches every activity in one transaction.
- **The account's zone still decides "today"**: the date slider's and Trends' windows end on today's date in the account's zone.

## Alternatives considered

- **The account's zone for everything.** Consistent and cheap, but a trip abroad shows the home clock's times and dates. Rejected as the end state.
- **The local zone for labels, the account's zone for grouping.** Rejected: near midnight a row's date would disagree with its slot in the slider, the same defect the other way round.
- **The viewing device's zone for everything.** Rejected: the same history would group differently on a laptop and a phone, and Trends would change as its owner travelled.
- **An offset from the source file.** Health Connect and FIT carry one; GPX and TCX don't, and an offset has no history of its own. Rejected in favour of one lookup that works for every source.
- **A Go library with embedded polygons** (such as `tzf`). No deploy step, but simplified polygons and a second copy of geography the database already holds in the same way for countries. Rejected in favour of the PostGIS pattern the country outlines already use.

## Consequences

- A deploy runs `seed-timezones` once after uploading the release (`DEPLOY.md` §6); until then every activity keeps its account's zone, which is how the app behaved before.
- Grouping and date filters compute `started_at AT TIME ZONE timezone` per row instead of one zone per query. At an account's hundreds to low thousands of activities, already narrowed by `(user_id, started_at)`, that costs nothing noticeable.
- Correct offsets depend on current tz data in three places: the database image (Debian's `tzdata`), the browser and the phone. A zone newer than the database's tz data is left out of the seed, and its area falls back to the account's zone until the image is updated.
- The polygons are a snapshot. A newer release that splits a zone off re-matches every activity when it's seeded.
- An activity entirely inside a Private location still carries a zone, which says roughly where it was. A zone is far coarser than the location it hides, and the account's own zone already says as much.