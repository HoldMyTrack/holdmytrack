# ADR-0001: Three independent ingest paths, none of them Strava

## Status

Accepted.

## Context

FitMap's whole product depends on getting activity data in. The obvious way to bootstrap that for a fitness-mapping product in 2026 is to build on the Strava API — it's the largest existing archive of exactly this data, and competitors doing exactly that (Statshunters, VeloViewer, Wandrer.earth) already prove the integration is straightforward.

The problem is structural, not technical: FitMap is explicitly positioned to compete with Strava (`VISION.md` §3.3), Strava's post-2024 terms restrict apps that replicate Strava features, and a free, single-maintainer, donation-funded product (`VISION.md` §6) cannot absorb the risk of a single vendor revoking API access and taking the whole product down with it. Almost every competitor in the space ingests via the Strava API and is, structurally, a Strava satellite — they inherit Strava's terms and die if Strava changes them.

Cloud-provider APIs generally carry the same risk in miniature: Garmin's Connect Developer Program historically requires a paid commercial licence of uncertain applicability to a free service; Wahoo and COROS require partner approval with real lead time and no guarantee; none of the three can be assumed available before building against them.

## Decision

Build three structurally independent ingest paths, so no single company's decision can take the product down:

1. **Path 1 — cloud-to-cloud** (Garmin, Wahoo, COROS, Oura): server-side OAuth pulls, each gated on that provider's own approval, built in whatever order approvals actually land.
2. **Path 2 — on-device sync** (Apple HealthKit, Android Health Connect): native mobile apps reading the platform's own health store, subject to real platform constraints (Health Connect route reads are foreground-only; Samsung specifically exposes no route geometry at all — verified against platform documentation, not assumed).
3. **Path 3 — direct file upload** (`.gpx`/`.fit`/`.tcx`, plus bulk-export archives including Strava's own export): no API terms, no licence, no permission model, no vendor who can revoke it — works for every service that offers an export, which is all of them, because GDPR requires it.

**Strava is deliberately not an ingest path.** Strava users reach FitMap through Path 3's export-archive import instead.

**Path 3 ships first, unconditionally**, before either of the other two, specifically because it needs nobody's permission. If Path 1 and Path 2 both fail to materialize, Path 3 alone still delivers the entire product to any user willing to export their history once — that is what the independence actually buys.

All three paths converge on one ingest pipeline from parse onward (`IMPLEMENTATION.md` §4.1 step 2) — they differ only in how bytes arrive, which is what keeps three paths affordable for one maintainer to build and keep working.

## Alternatives considered

- **Strava API as the primary or sole ingest path.** Rejected outright — the single biggest structural risk this product could take on, and directly undermines the "compete with Strava" positioning.
- **Cloud connectors (Path 1) first, file upload later.** Rejected — Path 1 is gated on other companies' approval timelines that can't be controlled or guaranteed, where Path 3 needs nobody's permission and can ship immediately.

## Consequences

- The product's very first usable version depends on no external approval at all, which is what let it actually ship (Phase 1, `VISION.md` §5.2) instead of waiting on Garmin/Wahoo/ COROS paperwork.
- Real functionality — automatic sync, power/cadence from cloud sources — is gated on approvals FitMap doesn't control and may never receive on the terms hoped for (`VISION.md` §4.1's own "validation gate"; tracked as open in `docs/ROADMAP.md`'s Phase 0).
- Three paths is three times the parsing/normalization surface to maintain, even though they converge quickly — cross-source deduplication (a second, related cost) becomes unavoidable the moment a second path actually exists (`IMPLEMENTATION.md` §4.6).
- Product copy has to stay honest about Path 2's real limits (Android is foreground-only, Samsung has no map at all) rather than implying parity across every source.