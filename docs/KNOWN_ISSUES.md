# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; now documented in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Export attribution missing from PNG exports — an ODbL compliance gap in already-shipped functionality

`style.ts:28` already documents that OSM/Protomaps attribution is mandatory, not decorative — the basemap is an ODbL "Produced Work," and the comment states credit "has to be visible on the map and on any export." But `exportMap.ts:165` sets `attributionControl: false` on the offscreen export map instance, and nothing else draws attribution onto any of the export pipeline's canvases before `toBlob()` — so FR-4.10's exported PNGs carry no attribution at all today, contradicting the code's own stated requirement.

- [ ] Draw `style.ts`'s own `ATTRIBUTION` text onto the *final* canvas — the one actually passed to `toBlob()` in `exportFramedImage` (`exportMap.ts:111`), after the crop-to-frame and scale-to-target steps, not the offscreen `renderOffscreen` canvas those steps read from — since frame position and preset scaling both happen after that point, and attribution needs to land correctly positioned/sized in the actual output regardless of either. Not optional or togglable: it's a license requirement, not a preference. `docs/ROADMAP.md`'s FitMap-logo item shares this same draw call site and pass — land them together if convenient, but that one is a courtesy and this one isn't optional.

---

### Mobile browser support is unusable, not just rough — reported directly by the user, not inferred from testing

The `@media (max-width: 768px)` layer at the end of `index.css` (`IMPLEMENTATION.md` §5.9, `SPEC.md` §13) was built and phone-emulated (Playwright `devices['iPhone 13']`) to look correct — zero horizontal overflow, the Activities panel collapsing to a bottom sheet, resize handle hidden, tap-based `Trends` tooltips — but the actual experience on a real device has been reported directly as unusable. Two real bugs were already found and fixed along the way (the histogram/date-range-picker footer forcing the whole layout viewport wider at ~390px, since fixed with `flex-wrap`; and a tap's browser-synthesized `mouseleave` re-clearing `Trends`' own tap state, since fixed by switching to `onPointerEnter`/`onPointerLeave` gated on `event.pointerType`) — so the gap isn't that nothing has been tried, it's that emulated verification isn't catching what a real device does.

- [ ] Re-verify on an actual phone, not just Playwright's iPhone 13 emulation — the emulation's zero-console-errors, zero-overflow result evidently isn't the same as usable.
- [ ] Scope was deliberately "core flows fully touch-usable," not full parity — the colored zone segments'/elevation profile's hover values (FR-4.8/FR-4.9) and the map-track-hover ↔ Activities-row-underline highlight (FR-4.1/FR-5.4) stay mouse-only by design, not part of this gap.
- [ ] `docs/ROADMAP.md`'s Phase 3 ("Finalized design + mobile browser support") folds real-device rework into the same pass as the icon/typography/design-token/animation work — this entry tracks the currently-broken state in the meantime, since it's a defect in already-shipped functionality, not unbuilt work.

---

### Pace/Heart Rate card visibly shrinks for activities with no elevation data — reported directly by the user, not inferred from testing

The floating `.track-metric-toggle` card (`MapView.tsx:875-906`) shows the Pace/Heart rate toggle buttons plus `TrackProfile.tsx`'s colored strip and elevation curve for whichever activity has row-click focus. `TrackProfile.tsx:106-117` only mounts the elevation `<svg>` (`.track-profile__elevation`, `height: 28px` + `margin-top: 2px` in `index.css:1367-1372`, ≈30px total) when `elevationAvailable` is true; when an activity's `elevation_available` comes back false from the server (`api.ts:975-976`: false for any activity with even one gap in elevation coverage — common for GPS-logger walks with no barometer, or any point missing a reading), the `<svg>` block is skipped entirely rather than rendered empty. Since `.track-metric-toggle`/`.track-profile` (`index.css:1329-1352`) have no fixed or min-height — the card is `display: flex; flex-direction: column`, sized purely by its children's natural heights plus `gap` — losing that ~30px block shrinks the whole card noticeably, since the rest of it (toggle buttons, the 8px-tall colored strip, the tooltip row) is small to begin with. The colored Pace/Heart-rate strip itself is unaffected and always renders; it's the card's overall height that visibly collapses.

- [ ] The code already solved this exact problem for the tooltip row one section down (`TrackProfile.tsx:118-123`): that row stays always-mounted and toggles `visibility: hidden` instead of conditionally rendering, specifically because the card is `position: absolute; bottom: ...`-anchored and a height change under the cursor causes a hide/show flicker loop. The elevation `<svg>` block never got the same treatment.
- [ ] Fix direction: always render `.track-profile__elevation` (reserving its ~30px) and only conditionally render the `<path>`/`<polyline>` content inside it when `elevationAvailable` is true, mirroring the tooltip row's pattern — rather than conditionally mounting the whole `<svg>`.
