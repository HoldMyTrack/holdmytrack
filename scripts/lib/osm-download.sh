# Sourced by the OpenStreetMap extract scripts (spots-extract.sh, bike-paths-extract.sh), not
# run on its own: downloads an OSM file to the current directory as source.osm.pbf and checks
# it. Two scripts run with the same work directory share one download.
#
# The planet's latest-* name is a redirect to a dated file; the checksum beside it names that
# dated file, so download both under fixed names and compare only the hash. Geofabrik serves
# a .md5 beside each extract too. A finished download is reused (and a cut-off one resumed), so
# a failed filter step doesn't mean downloading 85 GB again.

PLANET_URL=https://planet.openstreetmap.org/pbf/planet-latest.osm.pbf

log() { echo "$(date -u +%FT%TZ) $*"; }

# osm_download <url>: leaves a checked copy of <url> at ./source.osm.pbf.
osm_download() {
  local url=$1 src=source.osm.pbf want got
  log "fetching the checksum for $url"
  curl -fsSL -o source.md5 "$url.md5"
  want=$(awk '{print $1}' source.md5)

  if [[ -f "$src" && -f "$src.ok" && "$(cat "$src.ok")" == "$want" ]]; then
    log "already downloaded and checked: $src"
    return
  fi
  rm -f "$src.ok"
  log "downloading $url (resumable; re-run to continue after an interruption)"
  curl -fL -C - --retry 5 --retry-delay 30 -o "$src" "$url"
  log "checking md5"
  if command -v md5sum > /dev/null; then got=$(md5sum "$src" | awk '{print $1}'); else got=$(md5 -q "$src"); fi
  if [[ "$got" != "$want" ]]; then
    # A newer file may have been published mid-download, so resuming can't fix this.
    echo "md5 mismatch: got $got, want $want. Delete $PWD/$src and run again." >&2
    exit 1
  fi
  echo "$want" > "$src.ok"
}
