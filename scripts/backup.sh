#!/usr/bin/env bash
#
# Back up a prod deployment (docs/DEPLOY.md §11): dump Postgres, keep the dump on this host
# and copy it to the backup bucket, then sync the objects that can't be rebuilt into the same
# bucket. Run from the VPS, in the same directory as compose.prod.yml and .env.prod — nightly,
# from cron, with --nightly:
#
#   17 3 * * * root /srv/holdmytrack/scripts/backup.sh --nightly >> /var/log/holdmytrack-backup.log 2>&1
#
# and by hand, without it, before a deploy or a migration (DEPLOY.md §6), as a rollback point.
# Only a nightly run counts for the "Backup overdue" alert and fills the bucket's history: a
# deploy-day's manual runs would otherwise crowd it with near-identical dumps, and could hide a
# nightly job that stopped running.
#
# On this host, the newest KEEP_LOCAL_DUMPS dumps, whichever kind: the bucket holds the history,
# and a local copy is only for undoing the last deploy without fetching one.
#
# The backup bucket's layout:
#   postgres/daily/holdmytrack-<UTC stamp>.dump    every nightly run, kept KEEP_DAILY_DAYS
#   postgres/weekly/holdmytrack-<UTC stamp>.dump   Sunday's nightly run, kept KEEP_WEEKLY_DAYS
#   postgres/manual/holdmytrack-<UTC stamp>.dump   every manual run, kept KEEP_MANUAL_DAYS
#   objects/{raw,photos,avatars}/...               a mirror of the app bucket's own keys
#   objects-deleted/<UTC stamp>/...                what a run's sync removed or overwrote in
#                                                  objects/, kept KEEP_DELETED_DAYS
#
# fog/ and heatmap/ are left out, and so is the activity_tile_mask_data
# table's data (the masks' PNGs, ADR-0040): `rerender-coverage --masks` rebuilds all of them
# from the database and raw/ (DEPLOY.md §11's restore steps), so the dump doesn't grow with them.
#
set -euo pipefail
# A dump is the whole database, so it's root's alone like .env.prod: nothing written here is
# readable by anyone else (docs/DEPLOY.md §12).
umask 077

cd "$(dirname "$0")/.."

ENV_FILE=.env.prod
COMPOSE=(docker compose -f compose.prod.yml --env-file "$ENV_FILE")

KEEP_LOCAL_DUMPS=3
KEEP_DAILY_DAYS=14
KEEP_WEEKLY_DAYS=56
KEEP_MANUAL_DAYS=3
KEEP_DELETED_DAYS=30

nightly=false
case "${1:-}" in
  --nightly) nightly=true ;;
  "") ;;
  *) echo "Usage: $0 [--nightly]" >&2; exit 2 ;;
esac

[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found — run this from the deploy directory." >&2; exit 1; }

# One value from .env.prod. Read rather than sourced: the file isn't valid shell (an unquoted
# multi-word REDIRECT_DOMAINS), and only Compose parses it for the containers.
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1; }

[[ -n "$(env_value BACKUP_S3_BUCKET)" ]] || { echo "BACKUP_S3_BUCKET is empty in $ENV_FILE." >&2; exit 1; }
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

rclone() { "${COMPOSE[@]}" run --rm -T rclone "$@"; }

stamp=$(date -u +%Y%m%d-%H%M)
name="holdmytrack-$stamp.dump"
mkdir -p "$BACKUP_DIR/postgres"
chmod 700 "$BACKUP_DIR"
partial="$BACKUP_DIR/postgres/$name.partial"
trap 'rm -f "$partial"' EXIT

log "dumping Postgres to $BACKUP_DIR/postgres/$name"
"${COMPOSE[@]}" exec -T db sh -c 'pg_dump -Fc --exclude-table-data=activity_tile_mask_data -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > "$partial"
# A dump whose table of contents pg_restore can't read is no backup — fail here, not at the drill.
"${COMPOSE[@]}" exec -T db pg_restore --list < "$partial" > /dev/null
mv "$partial" "$BACKUP_DIR/postgres/$name"
log "dump is $(du -h "$BACKUP_DIR/postgres/$name" | cut -f 1)"

log "uploading the dump"
if $nightly; then
  rclone copyto "/backups/postgres/$name" "backup:postgres/daily/$name"
  if [[ $(date -u +%u) == 7 ]]; then
    rclone copyto "/backups/postgres/$name" "backup:postgres/weekly/$name"
  fi
else
  rclone copyto "/backups/postgres/$name" "backup:postgres/manual/$name"
fi

log "syncing raw/, photos/ and avatars/"
rclone sync data: backup:objects \
  --include '/raw/**' --include '/photos/**' --include '/avatars/**' \
  --backup-dir "backup:objects-deleted/$stamp" \
  --checksum --fast-list --stats 0 -v

log "pruning old copies"
# Newest first by the UTC stamp in the name, then everything past the first KEEP_LOCAL_DUMPS.
ls -1 "$BACKUP_DIR/postgres"/holdmytrack-*.dump | sort -r | tail -n +"$((KEEP_LOCAL_DUMPS + 1))" | while read -r old; do
  rm -f -- "$old"
done
prune_older() { rclone delete "$1" --min-age "$2" || [[ $? == 3 ]]; }  # 3: the folder isn't there yet
prune_older backup:postgres/daily "${KEEP_DAILY_DAYS}d"
prune_older backup:postgres/weekly "${KEEP_WEEKLY_DAYS}d"
prune_older backup:postgres/manual "${KEEP_MANUAL_DAYS}d"
# By the folder's stamp, not --min-age: a moved object keeps its own modification time, so an
# avatar uploaded a year ago and replaced last night would otherwise go on the next run.
cutoff=$(date -u -d "$KEEP_DELETED_DAYS days ago" +%Y%m%d-%H%M 2>/dev/null || date -u -v-"$KEEP_DELETED_DAYS"d +%Y%m%d-%H%M)
for dir in $(rclone lsf --dirs-only backup:objects-deleted); do
  if [[ "${dir%/}" < "$cutoff" ]]; then rclone purge "backup:objects-deleted/${dir%/}"; fi
done

# Only the nightly run: a manual one before a deploy mustn't hide a cron job that stopped.
if $nightly; then
  record_success holdmytrack_backup_last_success_timestamp_seconds backup
fi
log "backup done"
