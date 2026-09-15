#!/usr/bin/env bash
#
# Cut the FitMap basemap extract from a remote Protomaps planet build.
#
# Nothing close to the 138 GB planet build is downloaded: pmtiles extract reads the
# remote archive over HTTP range requests and pulls only the tiles inside BBOX.
#
# Usage:
#   ./scripts/build-basemap.sh              # cut the extract
#   DRY_RUN=1 ./scripts/build-basemap.sh    # report tile count + size, download nothing
#
set -euo pipefail

# Set the region. These two lines are the ONE thing to change.
REGION=ohio
BBOX=-84.85,38.35,-80.50,42.35          # minLon,minLat,maxLon,maxLat

# Pin a dated build key, never "latest": the bucket retains roughly the past week,
# so expect this key to age out and to need bumping. Build version 4.15.2 is the v4
# style channel that @protomaps/basemaps 5.7.2 expects. Check current keys at
# https://build-metadata.protomaps.dev/builds.json
#
# TODO: resolve the newest key automatically from that URL when BUILD is unset, keeping
# this pin as an explicit override — otherwise the next re-cut past the retention window
# fails with a 404 and no obvious cause (root README, "Known costs").
BUILD=https://build.protomaps.com/20260910.pmtiles

# z14, not z15. The planet build is z0-15 and the top level carries roughly three
# quarters of the bytes; for a statewide bbox that is the difference between a
# ~254,000-tile pyramid and a ~64,000-tile one. MapLibre overzooms vector tiles, so
# the only visible cost is slightly softer street labels at maximum zoom.
MAXZOOM=14

cd "$(dirname "$0")/.."
mkdir -p public/basemap
OUT="public/basemap/${REGION}.pmtiles"

if [[ -n "${DRY_RUN:-}" ]]; then
  pmtiles extract "$BUILD" "$OUT" \
    --bbox="$BBOX" \
    --maxzoom="$MAXZOOM" \
    --dry-run
  exit 0
fi

pmtiles extract "$BUILD" "$OUT" \
  --bbox="$BBOX" \
  --maxzoom="$MAXZOOM" \
  --download-threads=8

pmtiles verify "$OUT"
pmtiles show   "$OUT"    # tile count, zoom range, bounds
