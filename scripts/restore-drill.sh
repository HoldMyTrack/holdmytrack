#!/usr/bin/env bash
#
# Prove the backups restore (docs/DEPLOY.md §11): take the newest nightly dump from the backup
# bucket and the reference dump it pairs with (backup.sh), restore the two into a throwaway
# Postgres container with no network the way DEPLOY.md §11 restores them, and check that every object
# the restored database refers to is in the backup bucket too. Touches nothing the app uses —
# the live database is only read, for counts to compare. Run from the VPS, in the same
# directory as compose.prod.yml and .env.prod — monthly, from cron, and by hand after any
# change to backup.sh or the schema's object keys:
#
#   47 4 1 * * root /srv/holdmytrack/scripts/restore-drill.sh >> /var/log/holdmytrack-backup.log 2>&1
#
# Exits non-zero, and records no success for the "Restore drill overdue" alert, when
# anything fails.
#
set -euo pipefail
umask 077  # the downloaded dump is the whole database (backup.sh)

cd "$(dirname "$0")/.."

ENV_FILE=.env.prod
COMPOSE=(docker compose -f compose.prod.yml --env-file "$ENV_FILE")
CONTAINER=holdmytrack-restore-drill
MAX_DUMP_AGE=36h

[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found — run this from the deploy directory." >&2; exit 1; }

# One value from .env.prod; see backup.sh for why it isn't sourced.
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1; }

BACKUP_DIR=$(env_value BACKUP_DIR)
BACKUP_DIR=${BACKUP_DIR:-/srv/holdmytrack-backups}

log() { echo "$(date -u +%FT%TZ) $*"; }

# When this last succeeded, for the alerts (docs/DEPLOY.md §13): a file in Prometheus's text
# format that the alloy service's textfile collector reads. Written beside its final name and
# renamed, so a scrape never reads half of it.
record_success() {
  mkdir -p "$BACKUP_DIR/metrics"
  printf '# HELP %s Unix time of the last successful run.\n# TYPE %s gauge\n%s %s\n' "$1" "$1" "$1" "$(date +%s)" > "$BACKUP_DIR/metrics/$2.prom.tmp"
  mv "$BACKUP_DIR/metrics/$2.prom.tmp" "$BACKUP_DIR/metrics/$2.prom"
}

fail() { log "DRILL FAILED: $*"; exit 1; }
rclone() { "${COMPOSE[@]}" run --rm -T rclone "$@"; }
drill_psql() { docker exec "$CONTAINER" psql -U postgres -d restored -v ON_ERROR_STOP=1 -At -c "$1"; }
live_psql() { "${COMPOSE[@]}" exec -T db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "$0"' "$1"; }

mkdir -p "$BACKUP_DIR"
work=$(mktemp -d "$BACKUP_DIR/drill.XXXXXX")
trap 'docker rm -fv "$CONTAINER" > /dev/null 2>&1 || true; rm -rf "$work"' EXIT
docker rm -fv "$CONTAINER" > /dev/null 2>&1 || true

latest=$(rclone lsf --files-only backup:postgres/daily | sort | tail -n 1)
[[ -n "$latest" ]] || fail "no dump in backup:postgres/daily"
[[ -n "$(rclone lsf --files-only --max-age "$MAX_DUMP_AGE" backup:postgres/daily)" ]] ||
  fail "the newest dump, $latest, is older than $MAX_DUMP_AGE — is backup.sh running?"
# The newest reference dump no newer than it: what the tables it leaves out held then.
latest_stamp=${latest#holdmytrack-}; latest_stamp=${latest_stamp%.dump}
reference=""
while read -r name; do
  ref_stamp=${name#holdmytrack-reference-}; ref_stamp=${ref_stamp%.dump}
  [[ "$ref_stamp" > "$latest_stamp" ]] && break
  reference=$name
done < <(rclone lsf --files-only backup:postgres/reference < /dev/null | sort)
[[ -n "$reference" ]] || fail "no reference dump in backup:postgres/reference from before $latest"
log "restoring $latest with $reference"
rclone copyto "backup:postgres/daily/$latest" "/backups/$(basename "$work")/$latest"
rclone copyto "backup:postgres/reference/$reference" "/backups/$(basename "$work")/$reference"

# The image the live database runs, so the drill restores into exactly that PostGIS.
image=$(docker inspect -f '{{.Config.Image}}' "$("${COMPOSE[@]}" ps -q db)")
docker run -d --name "$CONTAINER" --network none -e POSTGRES_PASSWORD=drill \
  -v "$work:/drill:ro" "$image" > /dev/null
# -h 127.0.0.1: the image's first-start init runs a server on the socket only, so a TCP
# answer means the real one is up.
for _ in $(seq 60); do
  docker exec "$CONTAINER" pg_isready -q -h 127.0.0.1 -U postgres && break
  sleep 2
done
docker exec "$CONTAINER" pg_isready -q -h 127.0.0.1 -U postgres || fail "the drill's Postgres didn't start"

# From template0, so the image's own PostGIS setup in template1 doesn't collide with the
# dump's.
docker exec "$CONTAINER" createdb -U postgres -T template0 restored
# The schema, then the reference tables' data, then the rest, then the indexes and foreign
# keys, which then find every row they point at (DEPLOY.md §11).
restore() { docker exec "$CONTAINER" pg_restore -U postgres -d restored --exit-on-error --no-owner --no-acl "$@"; }
restore --section=pre-data "/drill/$latest" || fail "pg_restore of $latest's schema"
restore --data-only "/drill/$reference" || fail "pg_restore of $reference"
restore --section=data "/drill/$latest" || fail "pg_restore of $latest's data"
restore --section=post-data "/drill/$latest" || fail "pg_restore of $latest's indexes and constraints"

newest_migration=$(ls services/server/migrations/*.sql | sort | tail -n 1 | xargs basename)
log "newest migration: restored $(drill_psql 'SELECT max(filename) FROM schema_migrations'), checked out $newest_migration"

printf '%-18s %10s %10s\n' table restored live
for table in users activities activity_streams activity_photos stories privacy_zones spot_captures spots bike_paths admin_countries tz_parts; do
  printf '%-18s %10s %10s\n' "$table" "$(drill_psql "SELECT count(*) FROM $table")" "$(live_psql "SELECT count(*) FROM $table")"
done
[[ $(drill_psql 'SELECT count(*) FROM users') -gt 0 ]] || fail "the restored database has no users"
[[ $(drill_psql 'SELECT count(*) FROM admin_countries') -gt 0 ]] || fail "the restored database has no countries — is the reference dump empty?"
# The masks' PNGs are left out of the dump (backup.sh) but their table isn't.
[[ $(drill_psql 'SELECT count(*) FROM activity_tile_mask_data') -eq 0 ]] ||
  fail "the restored activity_tile_mask_data isn't empty — is backup.sh still leaving its data out?"

# Every key the restored database refers to, against what's in the backup bucket.
drill_psql "
  SELECT raw_payload_key FROM activities WHERE raw_payload_key IS NOT NULL
  UNION ALL SELECT image_key FROM activity_photos
  UNION ALL SELECT image_key || '-thumb' FROM activity_photos
  UNION ALL SELECT avatar_key FROM users WHERE avatar_key IS NOT NULL" | sort -u > "$work/expected"
rclone lsf -R --files-only --fast-list backup:objects | sort -u > "$work/present"
comm -23 "$work/expected" "$work/present" > "$work/missing"
# A key the app deleted after the dump was taken is in objects-deleted/<stamp>/ instead.
if [[ -s "$work/missing" ]]; then
  rclone lsf -R --files-only --fast-list backup:objects-deleted | cut -d / -f 2- | sort -u > "$work/deleted"
  comm -23 "$work/missing" "$work/deleted" > "$work/lost"
else
  : > "$work/lost"
fi
log "objects: $(wc -l < "$work/expected" | tr -d ' ') referenced, $(wc -l < "$work/lost" | tr -d ' ') not in the backup"
if [[ -s "$work/lost" ]]; then
  # Missing from the app bucket too means the database already pointed at nothing — a defect
  # to look into, but not one a better backup would have saved.
  rclone lsf -R --files-only --fast-list --include '/raw/**' --include '/photos/**' --include '/avatars/**' data: |
    sort -u > "$work/live"
  head -n 20 "$work/lost" | while read -r key; do
    if grep -qxF "$key" "$work/live"; then log "  not backed up: $key"; else log "  not in the app bucket either: $key"; fi
  done
  fail "$(wc -l < "$work/lost" | tr -d ' ') objects the restored database refers to aren't in the backup"
fi

record_success holdmytrack_restore_drill_last_success_timestamp_seconds restore_drill
log "drill passed: $latest restores, and every object it refers to is backed up"
