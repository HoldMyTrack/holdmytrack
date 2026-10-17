#!/usr/bin/env bash
# Warns when a change makes a source file over the soft limit any bigger. A file over the limit
# is a split candidate, not an error: this always exits 0, and a long file left untouched is silent.
#
# Compares each changed Go, Kotlin, TypeScript or TSX file in the working tree against the merge
# base with the base ref. Generated code, tests, migrations and the load-test driver are left out.
# Under GitHub Actions each warning is an annotation on the file.
# Usage: scripts/check-file-size.sh [base-ref] (default: origin/main).
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

LIMIT=600
base_ref="${1:-origin/main}"
if ! base=$(git merge-base "$base_ref" HEAD 2>/dev/null); then
  echo "check-file-size: no merge base with $base_ref; nothing checked" >&2
  exit 0
fi

git diff --name-only --diff-filter=AMR "$base" -- '*.go' '*.kt' '*.ts' '*.tsx' |
  grep -vE '_test\.go$|^apps/web/tests/|/src/(test|androidTest)/|/migrations/|^services/server/cmd/loadtest/|^apps/web/src/apiSchema\.ts$' |
  while read -r path; do
    [ -f "$path" ] || continue
    now=$(wc -l < "$path" | tr -d ' ')
    before=$(git show "$base:$path" 2>/dev/null | wc -l | tr -d ' ')
    [ "$now" -gt "$LIMIT" ] && [ "$now" -gt "$before" ] || continue
    message="$now lines (+$((now - before))), soft limit $LIMIT: a split candidate"
    if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
      echo "::warning file=$path,title=File size::$message"
    else
      echo "$path: $message"
    fi
  done
exit 0
