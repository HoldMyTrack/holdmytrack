#!/usr/bin/env bash
#
# Make the bike paths file (docs/DEPLOY.md §6): download an OpenStreetMap extract, check it, and
# filter it down to cycleways, bike-designated paths and mountain-bike trails as GeoJSON lines for
# `import-bike-paths`. Run OFF the server, like spots-extract.sh and for the same reason.
# Nothing here touches a deployment.
#
# Usage:
#   scripts/bike-paths-extract.sh <work-dir>            # the planet, for import-bike-paths --planet
#   scripts/bike-paths-extract.sh <work-dir> <pbf-url>  # a region (Geofabrik), without --planet
#
# Given the work-dir spots-extract.sh used, it reuses that download. The result is
# <work-dir>/bike-paths.geojsonseq.
#
set -euo pipefail

[[ $# -ge 1 && $# -le 2 ]] || { echo "Usage: $0 <work-dir> [<pbf-url>]  (the planet when no URL is given)" >&2; exit 2; }
command -v osmium > /dev/null || { echo "osmium not found: install osmium-tool (brew install osmium-tool, apt install osmium-tool)." >&2; exit 1; }
source "$(dirname "$0")/lib/osm-download.sh"

work=$1
url=${2:-$PLANET_URL}
mkdir -p "$work"
cd "$work"
src=source.osm.pbf
osm_download "$url"

# Keep every way that's a cycleway, carries bicycle=designated or has a mountain-bike rating,
# plus the nodes they need. The import narrows the last two to cycleways, paths, footways and
# bridleways (bikepaths.Kind, IMPLEMENTATION.md §4.24): a road with a designated bike lane, or a
# rated forest road, comes through here too.
log "filtering"
osmium tags-filter "$src" w/highway=cycleway w/bicycle=designated w/mtb:scale w/mtb:scale:imba \
  --overwrite -o bike-paths.osm.pbf

# -u type_id gives each feature the way id the import upserts by; lines only.
log "exporting"
osmium export bike-paths.osm.pbf -f geojsonseq -u type_id --geometry-types=linestring \
  --overwrite -o bike-paths.geojsonseq

# What to compare against the last run before importing (DEPLOY.md §6): a total far off the
# last one means a cut-short or mis-filtered file, which --planet would refuse anyway past 1%.
total=$(wc -l < bike-paths.geojsonseq | tr -d ' ')
log "done: $work/bike-paths.geojsonseq, $total ways, $(du -h bike-paths.geojsonseq | cut -f 1)"
for tag in '"highway":"cycleway"' '"bicycle":"designated"' '"mtb:scale"'; do
  printf '  %-36s %s\n' "$tag" "$(grep -c -F "$tag" bike-paths.geojsonseq || true)"
done
