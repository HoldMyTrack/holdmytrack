#!/usr/bin/env bash
#
# Make the Spots places file (docs/DEPLOY.md §6, ADR-0027): download an OpenStreetMap extract,
# check it, and filter it down to the five Spots categories as GeoJSON lines for
# `import-spots`. Run OFF the server, on any machine with osmium-tool, curl and the disk the
# source needs (about 100 GB free for the planet): the server has neither the disk nor the
# memory to filter the planet. Nothing here touches a deployment.
#
# Usage:
#   scripts/spots-extract.sh <work-dir>                 # the planet, for import-spots --planet
#   scripts/spots-extract.sh <work-dir> <pbf-url>       # a region (Geofabrik), without --planet
#
# Re-running reuses a finished download (and resumes a cut-off one), so a failed filter step
# doesn't mean downloading 85 GB again. The result is <work-dir>/spots.geojsonseq.
#
set -euo pipefail

PLANET_URL=https://planet.openstreetmap.org/pbf/planet-latest.osm.pbf

[[ $# -ge 1 && $# -le 2 ]] || { echo "Usage: $0 <work-dir> [<pbf-url>]  (the planet when no URL is given)" >&2; exit 2; }
command -v osmium > /dev/null || { echo "osmium not found: install osmium-tool (brew install osmium-tool, apt install osmium-tool)." >&2; exit 1; }

work=$1
url=${2:-$PLANET_URL}
mkdir -p "$work"
cd "$work"
src=source.osm.pbf

log() { echo "$(date -u +%FT%TZ) $*"; }

# The planet's latest-* name is a redirect to a dated file; the checksum beside it names that
# dated file, so download both under fixed names and compare only the hash. Geofabrik serves
# a .md5 beside each extract too.
log "fetching the checksum for $url"
curl -fsSL -o source.md5 "$url.md5"
want=$(awk '{print $1}' source.md5)

if [[ -f "$src" && -f "$src.ok" && "$(cat "$src.ok")" == "$want" ]]; then
  log "already downloaded and checked: $src"
else
  rm -f "$src.ok"
  log "downloading $url (resumable; re-run to continue after an interruption)"
  curl -fL -C - --retry 5 --retry-delay 30 -o "$src" "$url"
  log "checking md5"
  if command -v md5sum > /dev/null; then got=$(md5sum "$src" | awk '{print $1}'); else got=$(md5 -q "$src"); fi
  if [[ "$got" != "$want" ]]; then
    # A newer file may have been published mid-download, so resuming can't fix this.
    echo "md5 mismatch: got $got, want $want. Delete $work/$src and run again." >&2
    exit 1
  fi
  echo "$want" > "$src.ok"
fi

# Keep every node, way and relation in the five categories (spots.Category, IMPLEMENTATION.md
# §4.25), plus the nodes the ways and relations need for their geometry.
log "filtering"
osmium tags-filter "$src" \
  nwr/leisure=playground,dog_park \
  nwr/historic=monument,memorial,castle,ruins,fort,archaeological_site \
  nwr/tourism=viewpoint \
  --overwrite -o spots.osm.pbf

# -u type_id gives each feature the OSM id the import upserts by; points and areas only.
log "exporting"
osmium export spots.osm.pbf -f geojsonseq -u type_id --geometry-types=point,polygon \
  --overwrite -o spots.geojsonseq

# What to compare against the last run before importing (DEPLOY.md §6): a total far off the
# last one means a cut-short or mis-filtered file, which --planet would refuse anyway past 1%.
total=$(wc -l < spots.geojsonseq | tr -d ' ')
log "done: $work/spots.geojsonseq, $total places, $(du -h spots.geojsonseq | cut -f 1)"
for tag in '"leisure":"playground"' '"leisure":"dog_park"' '"historic":"monument"' '"historic":"memorial"' \
  '"historic":"castle"' '"historic":"ruins"' '"historic":"fort"' '"historic":"archaeological_site"' '"tourism":"viewpoint"'; do
  printf '  %-36s %s\n' "$tag" "$(grep -c -F "$tag" spots.geojsonseq || true)"
done
