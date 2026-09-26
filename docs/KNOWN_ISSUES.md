# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here — the same treatment the former root `BUGS.md` gave its one entry (the privacy-trim fallback that silently skipped short, sparse tracks; the trim itself has since been replaced by Private locations, and the lesson it taught — interpolate the cut point — is noted in `IMPLEMENTATION.md` §4.1) before that file was retired. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### Per-address rate limits are per-Caddy in production, so one limit is shared by every visitor

`demoLimiter` (demo starts) and `forgotPasswordLimiter` (reset emails), 5 per hour each, key on `clientIP` (`services/server/internal/httpapi/auth.go`), which takes `r.RemoteAddr` and deliberately ignores `X-Forwarded-For` — its comment says no reverse proxy sits in front of the server. In production one does: every request reaches `api` through Caddy (`apps/web/docker/Caddyfile`, ADR-0006), so `RemoteAddr` is the `web` container's address for every visitor, and the limit is effectively global — the sixth demo start in an hour, from anyone, is refused. Found by reading the code while the sign-in pages were moved to the server (`IMPLEMENTATION.md` §4.19), which reuse both limiters unchanged; not reproduced against the live site, since that would spend its real quota. `resendVerificationLimiter` keys on the account id and is unaffected.

- [ ] Trust `X-Forwarded-For` only from the proxy — Caddy sets it; take its client address when `RemoteAddr` is the `web` container (or a configured trusted proxy), and fall back to `RemoteAddr` otherwise, so a direct caller still can't spoof it.
- [ ] Verify against a local `compose.prod.yml`-shaped stack: two different client addresses should each get their own five demo starts an hour.

### A few server messages the apps show are still English in Russian

FR-13 translates every error message a person can cause, but three kinds of server text reach the screen untranslated, because the server writes them before it knows who will read them: an import's failure reason in the Sync tab's history (`sync.failed_with`, the worker's ingest error, stored on the job), the Android app's per-activity sync rejection reasons (`sync_activities.go`'s per-row `error`), and the web app's generic fallbacks for an error response with no body (`api.ts`'s "`… failed (status)`" messages, mainly a proxy or a crash between the app and the API). The first two need the stored reason to become a code the reader's side translates, rather than a sentence; the last only happens when something between the app and the API has already failed.

---

### The Android app has no email-verification screen, so an unverified account sees an empty map

The server gates the map's tiles and sync behind a verified email (`requireVerified`, `403 email_not_verified`), and the web sends an unverified account to `/verify-pending`. The Android app has no counterpart: an account that signs up there with email and password (when the server sends verification emails), or a new account made through Sign in with Facebook (FR-1.10, always unverified), opens the map with nothing on it and sync failing, and nothing on screen says why. It clears once the emailed link is clicked.

- [ ] Read `email_verified` from the session response and `GET /v1/auth/me`, and show a "check your email" screen with a resend button (`POST /v1/auth/resend-verification`) instead of the map until it's true.