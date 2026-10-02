#!/usr/bin/env bash
#
# Check a prod deployment from the inside (docs/DEPLOY.md §13) and report to a heartbeat
# monitor: the ping URL on success, its /fail on a problem, with this run's report as the body
# either way. A run that never pings, because the box or cron is down, is the monitor's own
# alert. Run from the VPS, in the same directory as compose.prod.yml and .env.prod — every five
# minutes, from cron:
#
#   */5 * * * * root /srv/holdmytrack/scripts/monitor.sh > /dev/null 2>&1
#
# What it checks, each over the five minutes since the last run:
#   - db, api, worker and web are running
#   - the ingest queue moves: no runnable job waiting longer than QUEUE_MAX_MINUTES
#   - errors on our side: api log lines at level ERROR (every 500 logs one) and Go panics, plus
#     jobs that failed for a reason other than the user's file (error_code 'internal', or a
#     non-ingest job) — at most MAX_ERRORS
#   - the disk is under DISK_MAX_PERCENT full, and the kernel's OOM killer killed nothing
#
# Prints the report and exits non-zero on a problem, so it can also be run by hand.
#
set -uo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=.env.prod
COMPOSE=(docker compose -f compose.prod.yml --env-file "$ENV_FILE")

WINDOW_MINUTES=5
QUEUE_MAX_MINUTES=30
MAX_ERRORS=0
DISK_MAX_PERCENT=85

[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found — run this from the deploy directory." >&2; exit 1; }

# One value from .env.prod; see backup.sh for why it isn't sourced.
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1; }
live_psql() { "${COMPOSE[@]}" exec -T db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -F " " -c "$0"' "$1"; }

report=()
problems=0
ok() { report+=("ok    $*"); }
bad() { report+=("FAIL  $*"); problems=$((problems + 1)); }

running=$("${COMPOSE[@]}" ps --status running --format '{{.Service}}' 2>/dev/null)
for service in db api worker web; do
  if grep -qx "$service" <<< "$running"; then ok "$service running"; else bad "$service not running"; fi
done

if grep -qx db <<< "$running"; then
  read -r waiting oldest < <(live_psql "
    SELECT count(*), COALESCE(EXTRACT(EPOCH FROM NOW() - min(run_after))::int / 60, 0)
    FROM jobs WHERE state = 'pending' AND run_after <= NOW()")
  if [[ -z "${oldest:-}" ]]; then
    bad "couldn't read the job queue"
  elif (( oldest > QUEUE_MAX_MINUTES )); then
    bad "queue: $waiting runnable, oldest waiting ${oldest} min"
  else
    ok "queue: $waiting runnable, oldest waiting ${oldest} min"
  fi
  failed_jobs=$(live_psql "
    SELECT count(*) FROM jobs
    WHERE state = 'failed' AND finished_at > NOW() - interval '$WINDOW_MINUTES minutes'
      AND (error_code IS NULL OR error_code = 'internal')")
else
  failed_jobs=0
fi

api_log=$("${COMPOSE[@]}" logs --no-log-prefix --since "${WINDOW_MINUTES}m" api 2>/dev/null)
api_errors=$(grep -cE '"level":"ERROR"|http: panic serving' <<< "$api_log")
errors=$((api_errors + ${failed_jobs:-0}))
if (( errors > MAX_ERRORS )); then
  bad "errors: $api_errors in the api log, ${failed_jobs:-0} failed jobs on our side"
  # The latest few, so the alert says what broke without a trip to the server.
  while read -r line; do report+=("      $line"); done < <(grep -E '"level":"ERROR"|http: panic serving' <<< "$api_log" | tail -n 5 | cut -c 1-300)
else
  ok "errors: none"
fi

disk=$(df -P / | awk 'NR == 2 { sub("%", "", $5); print $5 }')
if (( disk > DISK_MAX_PERCENT )); then bad "disk ${disk}% full"; else ok "disk ${disk}% full"; fi

oom=$(journalctl -k --since "-${WINDOW_MINUTES}min" --no-pager 2>/dev/null | grep -ci 'killed process')
if (( oom > 0 )); then
  bad "OOM killer: $oom processes killed"
  while read -r line; do report+=("      $line"); done < <(journalctl -k --since "-${WINDOW_MINUTES}min" --no-pager | grep -i 'killed process' | tail -n 3)
else
  ok "OOM killer: nothing killed"
fi

body=$(printf '%s\n' "${report[@]}")
echo "$body"

heartbeat=$(env_value MONITOR_HEARTBEAT_URL)
if [[ -n "$heartbeat" ]]; then
  url=$heartbeat
  (( problems > 0 )) && url="$heartbeat/fail"
  curl -fsS -m 10 --retry 3 -o /dev/null --data-binary "$body" "$url" || echo "heartbeat ping failed" >&2
fi
(( problems == 0 ))
