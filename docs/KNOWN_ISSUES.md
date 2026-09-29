# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; the trim itself has since been replaced by Private locations, and the lesson it taught — interpolate the cut point — is noted in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Trends leaves out empty weeks and months, so its bars don't show the 12 months `SPEC.md` FR-9 describes

FR-9 behavior 3 says the Profile page renders the trailing 12 months as one bar per bucket. `activityTrendsQuery` (`services/server/internal/httpapi/activities.go`) groups only the account's activities, so a week or month with none never comes back, and both clients draw one bar per period returned (`buildTrendBars`, `profile/TrendsChartView`). A history with two active weeks draws two bars filling the whole chart, each half its width, with nothing to show the other 50 weeks were empty; the axis's two dates are the first and last active period, not the window's ends. Found on the Android emulator against a local stack while porting the page; the web's `/profile` draws the same two bars.

- [ ] Fill the gaps — in the query (`generate_series` over the window's buckets, left-joined) or in each client — so every week or month of the window has a bar, at the 2px minimum when empty, and the axis shows the window's ends.
- [ ] Check the web page and the Android screen against an account with a few active weeks months apart.

### Per-address rate limits are per-Caddy in production, so one limit is shared by every visitor

`demoLimiter` (demo starts) and `forgotPasswordLimiter` (reset emails), 5 per hour each, key on `clientIP` (`services/server/internal/httpapi/auth.go`), which takes `r.RemoteAddr` and deliberately ignores `X-Forwarded-For` — its comment says no reverse proxy sits in front of the server. In production one does: every request reaches `api` through Caddy (`apps/web/docker/Caddyfile`, ADR-0006), so `RemoteAddr` is the `web` container's address for every visitor, and the limit is effectively global — the sixth demo start in an hour, from anyone, is refused. Found by reading the code while the sign-in pages were moved to the server (`IMPLEMENTATION.md` §4.19), which reuse both limiters unchanged; not reproduced against the live site, since that would spend its real quota. `resendVerificationLimiter` keys on the account id and is unaffected.

- [ ] Trust `X-Forwarded-For` only from the proxy — Caddy sets it; take its client address when `RemoteAddr` is the `web` container (or a configured trusted proxy), and fall back to `RemoteAddr` otherwise, so a direct caller still can't spoof it.
- [ ] Verify against a local `compose.prod.yml`-shaped stack: two different client addresses should each get their own five demo starts an hour.