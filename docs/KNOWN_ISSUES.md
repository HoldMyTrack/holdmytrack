# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; the trim itself has since been replaced by Private locations, and the lesson it taught — interpolate the cut point — is noted in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Mobile browser support is unusable, not just rough — reported directly by the user, not inferred from testing

The `@media (max-width: 768px)` layer at the end of `index.css` (`IMPLEMENTATION.md` §5.9, `SPEC.md` §16) was built and phone-emulated (Playwright `devices['iPhone 13']`) to look correct — zero horizontal overflow, the Activities panel collapsing to a bottom sheet, resize handle hidden, tap-based `Trends` tooltips — but the actual experience on a real device has been reported directly as unusable. Two real bugs were already found and fixed along the way (the histogram/date-range-picker footer forcing the whole layout viewport wider at ~390px, since fixed with `flex-wrap`; and a tap's browser-synthesized `mouseleave` re-clearing `Trends`' own tap state, since fixed by switching to `onPointerEnter`/`onPointerLeave` gated on `event.pointerType`) — so the gap isn't that nothing has been tried, it's that emulated verification isn't catching what a real device does.

- [ ] Re-verify on an actual phone, not just Playwright's iPhone 13 emulation — the emulation's zero-console-errors, zero-overflow result evidently isn't the same as usable.
- [ ] Scope was deliberately "core flows fully touch-usable," not full parity — the colored zone segments'/elevation profile's hover values (FR-4.8/FR-4.9) and the map-track-hover ↔ Activities-row-underline highlight (FR-4.1/FR-5.4) stay mouse-only by design, not part of this gap.
- [ ] `docs/ROADMAP.md`'s Phase 3 ("Finalized design + mobile browser support") folds real-device rework into the same pass as the icon/typography/design-token/animation work — this entry tracks the currently-broken state in the meantime, since it's a defect in already-shipped functionality, not unbuilt work.

---

### Per-address rate limits are per-Caddy in production, so one limit is shared by every visitor

`demoLimiter` (demo starts) and `forgotPasswordLimiter` (reset emails), 5 per hour each, key on `clientIP` (`services/server/internal/httpapi/auth.go`), which takes `r.RemoteAddr` and deliberately ignores `X-Forwarded-For` — its comment says no reverse proxy sits in front of the server. In production one does: every request reaches `api` through Caddy (`apps/web/docker/Caddyfile`, ADR-0006), so `RemoteAddr` is the `web` container's address for every visitor, and the limit is effectively global — the sixth demo start in an hour, from anyone, is refused. Found by reading the code while the sign-in pages were moved to the server (`IMPLEMENTATION.md` §4.19), which reuse both limiters unchanged; not reproduced against the live site, since that would spend its real quota. `resendVerificationLimiter` keys on the account id and is unaffected.

- [ ] Trust `X-Forwarded-For` only from the proxy — Caddy sets it; take its client address when `RemoteAddr` is the `web` container (or a configured trusted proxy), and fall back to `RemoteAddr` otherwise, so a direct caller still can't spoof it.
- [ ] Verify against a local `compose.prod.yml`-shaped stack: two different client addresses should each get their own five demo starts an hour.