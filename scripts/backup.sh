#!/usr/bin/env bash
#
# Back up a prod deployment (docs/DEPLOY.md §11): dump Postgres, keep the dump on this host
# and copy it to the backup bucket, then sync the objects that can't be rebuilt into the same
# bucket. Run from the VPS, in the same directory as compose.prod.yml and .env.prod — nightly,
# from cron:
#
#   17 3 * * * root /srv/holdmytrack/scripts/backup.sh >> /var/log/holdmytrack-backup.log 2>&1
#
# The backup bucket's layout:
#   postgres/daily/holdmytrack-<UTC stamp>.dump    every run, kept KEEP_DAILY_DAYS
#   postgres/weekly/holdmytrack-<UTC stamp>.dump   Sunday's run, kept KEEP_WEEKLY_DAYS
#   objects/{raw,photos,avatars}/...               a mirror of the app bucket's own keys
#   objects-deleted/<UTC stamp>/...                what a run's sync removed or overwrote in
#                                                  objects/, kept KEEP_DELETED_DAYS
#
# fog/, heatmap/ and activity-masks/ are left out: `rerender-coverage --masks` rebuilds all
# three from the database and raw/ (DEPLOY.md §11's restore steps).
#
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=.env.prod
COMPOSE=(docker compose -f compose.prod.yml --env-file "$ENV_FILE")

KEEP_LOCAL_DAYS=7
KEEP_DAILY_DAYS=14
KEEP_WEEKLY_DAYS=56
KEEP_DELETED_DAYS=30

[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found — run this from the deploy directory." >&2; exit 1; }

# One value from .env.prod. Read rather than sourced: the file isn't valid shell (an unquoted
# multi-word REDIRECT_DOMAINS), and only Compose parses it for the containers.
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1; }

[[ -n "$(env_value BACKUP_S3_BUCKET)" ]] || { echo "BACKUP_S3_BUCKET is empty in $ENV_FILE." >&2; exit 1; }
BACKUP_DIR=$(env_value BACKUP_DIR)
BACKUP_DIR=${BACKUP_DIR:-/srv/holdmytrack-backups}

log() { echo "$(date -u +%FT%TZ) $*"; }
rclone() { "${COMPOSE[@]}" run --rm -T rclone "$@"; }

stamp=$(date -u +%Y%m%d-%H%M)
name="holdmytrack-$stamp.dump"
mkdir -p "$BACKUP_DIR/postgres"
partial="$BACKUP_DIR/postgres/$name.partial"
trap 'rm -f "$partial"' EXIT

log "dumping Postgres to $BACKUP_DIR/postgres/$name"
"${COMPOSE[@]}" exec -T db sh -c 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > "$partial"
# A dump whose table of contents pg_restore can't read is no backup — fail here, not at the drill.
"${COMPOSE[@]}" exec -T db pg_restore --list < "$partial" > /dev/null
mv "$partial" "$BACKUP_DIR/postgres/$name"
log "dump is $(du -h "$BACKUP_DIR/postgres/$name" | cut -f 1)"

log "uploading the dump"
rclone copyto "/backups/postgres/$name" "backup:postgres/daily/$name"
if [[ $(date -u +%u) == 7 ]]; then
  rclone copyto "/backups/postgres/$name" "backup:postgres/weekly/$name"
fi

log "syncing raw/, photos/ and avatars/"
rclone sync data: backup:objects \
  --include '/raw/**' --include '/photos/**' --include '/avatars/**' \
  --backup-dir "backup:objects-deleted/$stamp" \
  --checksum --fast-list --stats 0 -v

log "pruning old copies"
find "$BACKUP_DIR/postgres" -name 'holdmytrack-*.dump' -mtime +"$KEEP_LOCAL_DAYS" -delete
rclone delete backup:postgres/daily --min-age "${KEEP_DAILY_DAYS}d"
rclone delete backup:postgres/weekly --min-age "${KEEP_WEEKLY_DAYS}d"
# By the folder's stamp, not --min-age: a moved object keeps its own modification time, so an
# avatar uploaded a year ago and replaced last night would otherwise go on the next run.
cutoff=$(date -u -d "$KEEP_DELETED_DAYS days ago" +%Y%m%d-%H%M 2>/dev/null || date -u -v-"$KEEP_DELETED_DAYS"d +%Y%m%d-%H%M)
for dir in $(rclone lsf --dirs-only backup:objects-deleted); do
  if [[ "${dir%/}" < "$cutoff" ]]; then rclone purge "backup:objects-deleted/${dir%/}"; fi
done

heartbeat=$(env_value BACKUP_HEARTBEAT_URL)
if [[ -n "$heartbeat" ]]; then
  curl -fsS -m 10 --retry 3 -o /dev/null "$heartbeat" || log "heartbeat ping failed"
fi
log "backup done"
