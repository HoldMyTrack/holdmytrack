#!/usr/bin/env bash
# A named volume is seeded once and never again, so changing package.json and rebuilding
# the image leaves the volume holding the old tree. Reconcile it offline by comparing the
# bind-mounted lockfile's hash against the marker written at image build.
set -euo pipefail
MARKER=/app/node_modules/.lockhash
WANT="$(sha256sum /app/package-lock.json | cut -d' ' -f1)"
[ "$(cat "$MARKER" 2>/dev/null || echo none)" = "$WANT" ] || {
  echo "holdmytrack: node_modules volume is stale; syncing from image" >&2
  # `npm ci` would rm -rf this directory and then fail EBUSY on the mount point.
  find /app/node_modules -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  cp -a /opt/deps/node_modules/. /app/node_modules/
}
exec "$@"
