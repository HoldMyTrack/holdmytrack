# ADR-0005: High-resolution export renders client-side, not in a headless worker pool

## Status

Accepted. Supersedes the original design in `IMPLEMENTATION.md` §5.5 for the first export slice specifically — that original design is not discarded, see Consequences.

## Context

`IMPLEMENTATION.md` §5.5 originally specified export as server-side work: render in the worker pool (not the API process), driven by the same shared style document every renderer uses (`docs/ARCHITECTURE.md` §2.1), delivered asynchronously, and rate-limited per user as the most expensive on-demand action an individual can trigger.

That design assumes export needs infrastructure the rest of the system doesn't already have — a headless MapLibre renderer running server-side. But the map is already fully client-rendered via MapLibre GL JS in every browser that loads it. For the first export slice (a high- resolution PNG of the current view), standing up a headless render pipeline would be real new infrastructure built specifically to reproduce something the browser sitting right there can already do.

## Decision

**The first export slice renders client-side.** A second, temporary, hidden MapLibre `Map` instance is pointed at a larger off-screen container, replays the same overlays (fog/heatmap/ tracks, mode, filters) the visible map already has active, and its canvas is captured directly — see `apps/web/src/map/exportMap.ts`'s own doc comment for the mechanism. This was proposed and confirmed directly with the user as a deliberate deviation from the documented plan before being built, not decided unilaterally.

Because rendering happens in the visitor's own browser, it costs the server nothing — no worker-pool job, no queue, nothing to rate-limit the way the original design correctly anticipated for a server-side render.

## Alternatives considered

- **Server-side headless render in the worker pool** (the original design). Not rejected as wrong — explicitly kept on record as the right design for a case this client-side path can't cover: a specific calibrated-print-DPI product, or an output large enough that a single browser tab's own GPU/memory can't reliably hold one temporary map instance. If that ceiling ever matters, §5.5's original specification is the design to build against, not a discarded idea to redesign from scratch.

## Consequences

- Zero server cost per export, and zero new infrastructure to operate for the feature — a direct fit for a free, community-funded product where every added server-side cost is a cost someone has to keep funding (`VISION.md` §6).
- A real, honestly-named ceiling: this path can only export at whatever resolution the browser's own canvas will allocate — generously larger than a screenshot, but not a calibrated print DPI against a known physical output size, and bounded by what one browser tab's GPU/memory can hold for a second, temporary map instance.
- Nothing to rate-limit for this slice, which simplifies it relative to the original design — but also means a future server-side render (if the ceiling above is ever hit) still needs that rate-limiting built, not inherited from this path.
- The exported image can only reflect what the client already knows how to draw — a per- activity view not otherwise composited on the live map (colored zone segments for a single focused activity, at the time this was built) isn't reflected in an export even when visible on screen, since export replays the map's own overlay-construction functions rather than screenshotting arbitrary DOM.