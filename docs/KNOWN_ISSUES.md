# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; the trim itself has since been replaced by Private locations, and the lesson it taught — interpolate the cut point — is noted in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Per-address rate limits are per-Caddy in production, so one limit is shared by every visitor

`demoLimiter` (demo starts) and `forgotPasswordLimiter` (reset emails), 5 per hour each, key on `clientIP` (`services/server/internal/httpapi/auth.go`), which takes `r.RemoteAddr` and deliberately ignores `X-Forwarded-For` — its comment says no reverse proxy sits in front of the server. In production one does: every request reaches `api` through Caddy (`apps/web/docker/Caddyfile`, ADR-0006), so `RemoteAddr` is the `web` container's address for every visitor, and the limit is effectively global — the sixth demo start in an hour, from anyone, is refused. Found by reading the code while the sign-in pages were moved to the server (`IMPLEMENTATION.md` §4.19), which reuse both limiters unchanged; not reproduced against the live site, since that would spend its real quota. `resendVerificationLimiter` keys on the account id and is unaffected.

- [ ] Trust `X-Forwarded-For` only from the proxy — Caddy sets it; take its client address when `RemoteAddr` is the `web` container (or a configured trusted proxy), and fall back to `RemoteAddr` otherwise, so a direct caller still can't spoof it.
- [ ] Verify against a local `compose.prod.yml`-shaped stack: two different client addresses should each get their own five demo starts an hour.