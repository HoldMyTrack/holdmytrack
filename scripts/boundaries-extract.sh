#!/usr/bin/env bash
#
# Make the country and region outlines file (docs/DEPLOY.md §6, ADR-0034): cut the land-clipped
# countries, dependencies and regions out of a pinned Overture Maps release with DuckDB, for
# `seed-admin-boundaries`. Run OFF the server, on any machine with the duckdb CLI
# (brew install duckdb) and a few GB free: it reads about 4.5 GB of GeoParquet from Overture's
# public S3 bucket, no account needed. Nothing here touches a deployment.
#
# Usage:
#   scripts/boundaries-extract.sh <work-dir>
#
# The result is <work-dir>/<file>, named after the release; geo.BoundariesKey names the same file,
# so moving to a newer release means changing both. Overture deletes a release a few months after
# the next one, which is why the deployment reads its own copy from the app bucket.
#
set -euo pipefail

RELEASE=2026-09-23.0

[[ $# -eq 1 ]] || { echo "Usage: $0 <work-dir>" >&2; exit 2; }
command -v duckdb > /dev/null || { echo "duckdb not found: install it (brew install duckdb, or https://duckdb.org/docs/installation)." >&2; exit 1; }

work=$1
mkdir -p "$work"
out="$work/overture-$RELEASE-boundaries.csv.gz"

log() { echo "$(date -u +%FT%TZ) $*"; }

log "extracting Overture $RELEASE divisions into $out (a few minutes)"
sql=$(cd "$(dirname "$0")" && pwd)/boundaries-extract.sql
printf "SET VARIABLE release = '%s';\nSET VARIABLE out = '%s';\n.read %s\n" "$RELEASE" "$out" "$sql" | duckdb

# What to compare against the last extract before uploading: about 270 countries and dependencies
# and 3,900 regions in 2026-09.
log "done: $out, $(du -h "$out" | cut -f 1)"
gzip -dc "$out" | cut -d , -f 1 | sort | uniq -c
