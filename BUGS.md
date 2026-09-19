# Known bugs

## ~~Privacy-trim fallback silently skips trimming on short, sparse tracks~~ — fixed

`services/server/internal/ingest/trim.go`'s `TrimEndpoints` is supposed to strip the first/last 200m (§7's "non-negotiable" endpoint privacy trim) off every activity's trajectory, walked distance, before it's ever persisted or rendered. It works correctly for most activities, but degrades to a silent no-op for a real, reachable class of input.

**Root cause**: the function walks forward from the start and backward from the end, in whole-point steps (no interpolation within a segment), advancing an index each time until 200m of cumulative distance has been crossed from that side. If the track has few, widely-spaced points, a single segment can be much longer than 200m — so the "200m in" index from one side can land past (or at) the "200m in" index from the other side, even when the track's total length is well over 400m. When that happens:

```go
if start >= end {
    // The whole track is shorter than 2x the trim radius. Keep the endpoints rather
    // than emit an empty activity — an over-aggressive trim on a short track is a
    // worse failure mode than under-trimming by a few meters.
    return points
}
return points[start : end+1]
```

The fallback comment's premise ("the whole track is shorter than 2x the trim radius") is only sometimes true — the actual trigger is the *point spacing*, not the track's real length. A short track with dense points trims fine; a longer track with only a handful of GPS points can hit this same fallback and return **completely untrimmed**, endpoints included.

**Observed impact**: found while seeding the Demo Customer account's 611-activity history (`internal/httpapi/demo_presets.go`) — one activity (`Car Ride 2026-09-06`, sourced from `Car Ride-1.gpx`, 8 raw points over ~890m) came back with its start point 4.4m from the account's real home coordinate, i.e. essentially unclipped, instead of the expected ~200m+. Every other seeded activity trimmed correctly (170–470m from home). This is exactly the class of input (a short errand drive with few recorded points) a real account's real upload could also produce — this isn't a demo-only artifact, it's a live privacy gap in shipped, "non-negotiable" functionality.

**Suggested fix direction** (not implemented — flagged here for later, at the user's request): interpolate a synthetic point at the exact 200m mark along the crossing segment instead of only ever landing on existing point indices, so the trim result no longer depends on how sparse the recording was. Re-check the `start >= end` fallback's condition against the track's *actual total length* (< 400m) rather than against where the two index walks happen to land.

**Status**: fixed. `TrimEndpoints` now walks the same way but interpolates a synthetic point at exactly `trimM` along the crossing segment instead of snapping to whichever existing point is nearest, and the short-track fallback checks the track's real total walked length (`<= 2*trimM`) rather than where the two index walks happen to land. `Car Ride 2026-09-06` now trims to 200.1m from home (previously 4.4m); a full re-seed of the Demo Customer account's 611 activities found zero remaining cases with a start within 50m of home on any activity longer than 400m. Covered by `internal/ingest/trim_test.go`, including a regression test built from this exact activity's real coordinates.
