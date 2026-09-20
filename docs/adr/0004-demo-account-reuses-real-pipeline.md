# ADR-0004: The no-signup demo reuses the real account pipeline, bounded by a TTL

## Status

Partially superseded. The real-pipeline decision below (reuse the exact same upload/ingest/privacy/fog code a real account uses, rather than a parallel client-side-only path) still holds. The specific mechanism described in "Decision" and "Consequences" below — a fresh, ephemeral `users` row per demo visitor, a 24-hour TTL, a background purge, and converting to a real account by updating that row in place — does not: the convert-in-place path was retired (a demo account can no longer become a real one; FR-2.3 is an ordinary new signup instead), and the per-visitor ephemeral row was replaced by one shared, non-expiring Demo Customer account every demo session opens against — see `docs/SPEC.md` FR-2.1–FR-2.3 and `docs/IMPLEMENTATION.md` §4.10 for the current behavior. The purge sweep (`internal/worker/demo_purge.go`) described below still exists and still runs, but has nothing left to act on under the current design.

Originally: Accepted. Supersedes an earlier design recorded in the same section of `IMPLEMENTATION.md` §7.

## Context

`VISION.md` §8.2 calls the no-signup demo — "drag a file in, see your fog map, no signup" — the single best marketing asset the product has: shareability at zero friction, screenshot- friendly, and the reason file upload (Path 3, ADR-0001) ships before anything gated on another company's approval.

The original design treated this as a marketing surface, not a storage tier: parse the dropped file entirely in the browser and persist nothing server-side, specifically to avoid the demo becoming an anonymous, unbounded upload endpoint with no account behind it.

Once actually designed against the real ingest pipeline, that plan had a real cost: it meant building and maintaining a second, parallel, client-side-only ingest/render path — duplicate parsing, duplicate privacy trimming, duplicate fog rendering — solely to keep a demo session from touching the server, alongside the one real pipeline every other account already uses.

## Decision

**A demo session gets a real, ephemeral `users` row and a real session**, created by the exact same code path as a real signup (`IMPLEMENTATION.md` §4.10) — upload, ingest, privacy trimming (ADR-0002), fog rendering, tile serving all run completely unchanged for a demo visitor, because a demo account is not a special case anywhere in that pipeline.

What actually bounds it from becoming an anonymous, unbounded storage tier is not "no persistence" but a **24-hour TTL plus a per-IP rate limit on demo creation** (`demoLimiter`, `IMPLEMENTATION.md` §4.10) and an automatic purge: `demo_expires_at` marks the row ephemeral, and a background sweep every 5 minutes deletes every expired demo account's database rows (cascaded) and its object-storage keys (`raw/{userID}/`, `fog/{userID}/`, `heatmap/{userID}/`) — not just the row.

A demo visitor can convert it into a permanent account (`FR-2.3`) without losing anything already uploaded: the same account row is updated in place (email/password set, `demo_expires_at` cleared) rather than migrating data to a new one.

## Alternatives considered

- **Client-side-only parsing, nothing persisted server-side** (the original plan). Rejected — the cost wasn't in the idea, it was in the duplicate implementation surface: a second ingest/privacy/fog pipeline, hand-kept in sync with the real one forever, for a feature whose entire point is to look and behave identically to the real product.
- **A server-side demo with no expiry, cleaned up manually.** Never seriously considered — reintroduces exactly the "anonymous unbounded upload endpoint" risk the original design was trying to avoid in the first place, just without the mitigation.

## Consequences

- The demo is provably identical to the real product, because it *is* the real product, running under a time-boxed account — no separate code path to keep matching feature-for- feature as the real pipeline evolves.
- Privacy enforcement (ADR-0002) applies identically to a demo account with nothing demo-specific to implement or forget.
- A demo session now genuinely holds a visitor's real (if temporary) uploaded data server-side, which is a real privacy posture to be honest about, not a "nothing is stored" claim — the 24-hour TTL and full-deletion purge are what make that posture acceptable, not incidental to it.
- The purge sweep (`internal/worker/demo_purge.go`) is now permanent, load-bearing infrastructure, not a one-off cleanup script — if it silently stopped running, demo accounts would accumulate as real, permanent, anonymous storage indefinitely.
- Per-IP rate limiting on demo creation (`demoLimiter`) is required precisely because this endpoint is reachable with no credentials at all and now has a real, if bounded, resource cost behind it.