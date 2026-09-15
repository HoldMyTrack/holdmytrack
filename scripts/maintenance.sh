#!/usr/bin/env bash
#
# Flip prod maintenance mode on/off (docs/DEPLOY.md's "Maintenance mode" section,
# apps/web/docker/Caddyfile). Run from the VPS, in the same directory as compose.prod.yml
# and .env.prod.
#
# Usage:
#   ./scripts/maintenance.sh on      # show the maintenance page, recreate `web`
#   ./scripts/maintenance.sh off     # resume normal routing, recreate `web`
#   ./scripts/maintenance.sh status  # print the current value
#
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=.env.prod
COMPOSE=(docker compose -f compose.prod.yml --env-file "$ENV_FILE")

usage() {
  echo "Usage: $0 {on|off|status}" >&2
  exit 1
}

[[ $# -eq 1 ]] || usage
[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found — run this from the deploy directory." >&2; exit 1; }

set_mode() {
  local value=$1
  if grep -q '^MAINTENANCE_MODE=' "$ENV_FILE"; then
    sed -i.bak "s/^MAINTENANCE_MODE=.*/MAINTENANCE_MODE=${value}/" "$ENV_FILE" && rm -f "${ENV_FILE}.bak"
  else
    echo "MAINTENANCE_MODE=${value}" >> "$ENV_FILE"
  fi
  "${COMPOSE[@]}" up -d web
}

case "$1" in
  on)
    set_mode true
    echo "Maintenance mode ON. /healthz still reflects real api/db status — poll it before turning this off."
    ;;
  off)
    set_mode false
    echo "Maintenance mode OFF."
    ;;
  status)
    grep '^MAINTENANCE_MODE=' "$ENV_FILE" || echo "MAINTENANCE_MODE=false (unset, using default)"
    ;;
  *)
    usage
    ;;
esac
