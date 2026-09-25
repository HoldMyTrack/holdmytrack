# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; the trim itself has since been replaced by Private locations, and the lesson it taught — interpolate the cut point — is noted in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Mobile browser support is unusable, not just rough — reported directly by the user, not inferred from testing

The `@media (max-width: 768px)` layer at the end of `index.css` (`IMPLEMENTATION.md` §5.9, `SPEC.md` §17) was built and phone-emulated (Playwright `devices['iPhone 13']`) to look correct — zero horizontal overflow, the Activities panel collapsing to a bottom sheet, resize handle hidden, tap-based `Trends` tooltips — but the actual experience on a real device has been reported directly as unusable. Two real bugs were already found and fixed along the way (the histogram/date-range-picker footer forcing the whole layout viewport wider at ~390px, since fixed with `flex-wrap`; and a tap's browser-synthesized `mouseleave` re-clearing `Trends`' own tap state, since fixed by switching to `onPointerEnter`/`onPointerLeave` gated on `event.pointerType`) — so the gap isn't that nothing has been tried, it's that emulated verification isn't catching what a real device does.

- [ ] Re-verify on an actual phone, not just Playwright's iPhone 13 emulation — the emulation's zero-console-errors, zero-overflow result evidently isn't the same as usable.
- [ ] Scope was deliberately "core flows fully touch-usable," not full parity — the colored zone segments'/elevation profile's hover values (FR-4.8/FR-4.9) and the map-track-hover ↔ Activities-row-underline highlight (FR-4.1/FR-5.4) stay mouse-only by design, not part of this gap.
- [ ] `docs/ROADMAP.md`'s Phase 3 ("Finalized design + mobile browser support") folds real-device rework into the same pass as the icon/typography/design-token/animation work — this entry tracks the currently-broken state in the meantime, since it's a defect in already-shipped functionality, not unbuilt work.

---

### Per-address rate limits are per-Caddy in production, so one limit is shared by every visitor

`demoLimiter` (demo starts) and `forgotPasswordLimiter` (reset emails), 5 per hour each, key on `clientIP` (`services/server/internal/httpapi/auth.go`), which takes `r.RemoteAddr` and deliberately ignores `X-Forwarded-For` — its comment says no reverse proxy sits in front of the server. In production one does: every request reaches `api` through Caddy (`apps/web/docker/Caddyfile`, ADR-0006), so `RemoteAddr` is the `web` container's address for every visitor, and the limit is effectively global — the sixth demo start in an hour, from anyone, is refused. Found by reading the code while the sign-in pages were moved to the server (`IMPLEMENTATION.md` §4.19), which reuse both limiters unchanged; not reproduced against the live site, since that would spend its real quota. `resendVerificationLimiter` keys on the account id and is unaffected.

- [ ] Trust `X-Forwarded-For` only from the proxy — Caddy sets it; take its client address when `RemoteAddr` is the `web` container (or a configured trusted proxy), and fall back to `RemoteAddr` otherwise, so a direct caller still can't spoof it.
- [ ] Verify against a local `compose.prod.yml`-shaped stack: two different client addresses should each get their own five demo starts an hour.

### Russian headings fall back to a generic serif

Fraunces, the heading and wordmark font (`tokens.css`'s `--fm-font-serif`, loaded from Google Fonts), has no Cyrillic glyphs. In Russian (FR-13), every heading — page titles, the Activities panel's tabs, section headings, the tagline — renders in the browser's default serif instead, so it looks different from the English design, and different again from one browser to another. Inter, the body font, covers Cyrillic, so body text is unaffected. Found in the Russian Playwright pass for `IMPLEMENTATION.md` §4.21. The fix is a design decision, not a code change: pick a serif with Cyrillic (the Fraunces look-alikes on Google Fonts that have it), or a Cyrillic fallback named in `--fm-font-serif` ahead of the generic `serif`.

### A few server messages the apps show are still English in Russian

FR-13 translates every error message a person can cause, but three kinds of server text reach the screen untranslated, because the server writes them before it knows who will read them: an import's failure reason in the Sync tab's history (`sync.failed_with`, the worker's ingest error, stored on the job), the Android app's per-activity sync rejection reasons (`sync_activities.go`'s per-row `error`), and the web app's generic fallbacks for an error response with no body (`api.ts`'s "`… failed (status)`" messages, mainly a proxy or a crash between the app and the API). The first two need the stored reason to become a code the reader's side translates, rather than a sentence; the last only happens when something between the app and the API has already failed.