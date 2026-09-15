# ADR-0006: Minimal deployment serves frontend and API from one origin via Caddy

## Status

Accepted.

## Context

The dev stack (`compose.yaml`) runs the frontend and API as genuinely separate origins — Vite's dev server on `:5173`, the Go API on `:8080` — which is why `httpapi/server.go` carries a credentialed CORS allowlist at all: a cross-origin request carrying a session cookie needs the server's explicit permission, checked against a fixed list of allowed origins (`corsAllowedOrigins`), or the browser drops the cookie.

Standing up a first minimal production deployment (`docs/DEPLOY.md`, `IMPLEMENTATION.md` §5.8) needed a reverse proxy in front of the API regardless (for TLS), and a decision about whether the built frontend continues living on a separate origin from the API in production too, the way local dev already does — which would mean `corsAllowedOrigins` needs a real production entry added, and every credentialed request in production stays cross-origin.

## Decision

**One Caddy container serves both jobs from a single origin**: it terminates TLS, serves the built static frontend directly, and reverse-proxies `/v1/*`, `/tiles/*`, and `/healthz` through to the `api` container — all under the same domain (`apps/web/docker/Caddyfile`). The production frontend build sets `VITE_API_BASE_URL=""` unconditionally, so every API call is a plain root-relative request resolved against whatever domain it's actually served from, with no per-deployment configuration needed.

Caddy specifically (not nginx) for a deployment described as *minimal*: it obtains and renews its own Let's Encrypt certificate automatically, with no separate certbot/cron step to operate.

## Alternatives considered

- **Keep frontend and API on separate origins in production**, add a real production entry to `corsAllowedOrigins`. Rejected — same-origin requests are never cross-origin requests, so CORS configuration for the primary app simply isn't a concern that needs to exist in this topology at all; adding a production CORS entry would have been solving a problem this architecture doesn't have to have.
- **nginx as the reverse proxy.** Rejected specifically for a *minimal* deployment — nginx needs a separate, hand-operated TLS/certbot setup; Caddy needs none.
- **A separate reverse-proxy service plus a separate static-file server.** Rejected as unnecessary complexity for this scale — one Caddy container already does both jobs correctly, and `compose.prod.yml`'s whole point is the smallest topology that's still genuinely production-shaped (four services total: `db`, `api`, `worker`, `web`).

## Consequences

- `corsAllowedOrigins` needs no production entry, and stays exactly what it already was: a dev- only allowlist for the handful of `localhost` origins the dev/test stack actually uses.
- Every credentialed request in production is same-origin by construction — a stronger, simpler security posture than a cross-origin one that has to be gotten right, not a bolt-on.
- The frontend and API can no longer be deployed to genuinely different domains without revisiting this decision — if that's ever wanted (a separate marketing site, a mobile webview hitting the API from its own origin), `corsAllowedOrigins` would need a real entry added at that point, which this decision explicitly defers rather than building speculatively now.
- The production frontend build is not portable across domains without a rebuild in the sense that matters least (the API base is relative, so it never needs to be), but the basemap origin (`VITE_BASEMAP_ORIGIN`) is still a real build-time value when it's used, for the separate reason that the basemap archive is deliberately excluded from every Docker build context (`docs/DEPLOY.md` §5).