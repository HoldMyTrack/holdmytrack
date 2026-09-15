# syntax=docker/dockerfile:1
# bookworm, not alpine: Playwright ships no musl Chromium build, and that settles it.
FROM node:22.22.3-bookworm-slim AS base
ENV NPM_CONFIG_UPDATE_NOTIFIER=false NPM_CONFIG_FUND=false
WORKDIR /app

FROM base AS deps
COPY package.json package-lock.json ./
RUN npm ci
RUN sha256sum package-lock.json | cut -d' ' -f1 > /app/node_modules/.lockhash

FROM base AS dev
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
# `npm run basemap` with no host binary. The path is /go-pmtiles — verified from the
# image config; it is NOT /pmtiles.
COPY --from=protomaps/go-pmtiles:v1.31.2 /go-pmtiles /usr/local/bin/pmtiles
# Staged outside /app/node_modules, because that path is a volume mount at runtime.
COPY --from=deps /app/node_modules /opt/deps/node_modules
COPY docker/entrypoint.sh /usr/local/bin/fitmap-entrypoint
RUN chmod +x /usr/local/bin/fitmap-entrypoint
EXPOSE 5173 4173
ENTRYPOINT ["fitmap-entrypoint"]
CMD ["npm", "run", "dev"]

FROM dev AS test
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN npx --prefix /opt/deps playwright install --with-deps chromium \
 && chmod -R a+rX /ms-playwright
CMD ["npm", "run", "verify:map"]

# Production build. Same-origin default: docker/Caddyfile (the `serve` stage below)
# reverse-proxies /v1/*, /tiles/* and /healthz onto the same origin this bundle is served
# from, so a relative API base works regardless of the eventual domain — no per-deployment
# build ARG needed for VITE_API_BASE_URL, unlike VITE_BASEMAP_ORIGIN below.
#
# VITE_BASEMAP_ORIGIN defaults to empty (same-origin, config.ts's basemapOrigin()) — but
# .dockerignore excludes every *.pmtiles file from this build context on purpose (see its
# own comment: "the container reads it through the bind mount at runtime, never from the
# image"), so this image never has the basemap archive to serve locally by default. Either
# leave this unset and bind-mount the real archive into the `web` container's /srv/web/
# basemap/ at deploy time (simplest, no rebuild needed to change it), or set this build arg
# to wherever the archive actually lives (object storage/a CDN) and rebuild — see
# DEPLOY.md for both.
FROM base AS build
ARG VITE_BASEMAP_ORIGIN=""
# The build's git short-SHA (compose.prod.yml's GIT_SHA build arg) — baked into the bundle
# so VersionBanner.tsx can detect a tab left open across a deploy by comparing it against
# /healthz's own version field.
ARG GIT_SHA=""
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV VITE_API_BASE_URL=""
ENV VITE_BASEMAP_ORIGIN=$VITE_BASEMAP_ORIGIN
ENV VITE_GIT_SHA=$GIT_SHA
RUN npm run build

# Caddy serves the built static assets *and* reverse-proxies API/tile requests to `api` —
# one container doing both jobs, matching compose.prod.yml's minimal 4-service topology
# (web, api, worker, db) rather than a separate reverse-proxy service.
FROM caddy:2-alpine AS serve
COPY --from=build /app/dist /srv/web
COPY docker/Caddyfile /etc/caddy/Caddyfile
