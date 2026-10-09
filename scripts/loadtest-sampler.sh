#!/bin/bash
# Samples a deployment every 5 s while a load test runs (docs/PERFORMANCE.md "Running a
# session"): host load and memory, each container's CPU and memory, the job queue by kind and
# state, and the database's size and connections. Copy it to the server and run it there,
# keeping the output on your machine:
#
#   scp scripts/loadtest-sampler.sh holdmytrack:/tmp/ && ssh holdmytrack 'bash /tmp/loadtest-sampler.sh' > sampler.log
#
# Stop it with `pkill -f "^bash /tmp/loadtest-sampler.sh"`: a bare `pkill -f loadtest-sampler.sh`
# over ssh also matches the remote shell running that very command and kills it.
cd /srv/holdmytrack || exit 1
psql() {
  docker compose -f compose.prod.yml --env-file .env.prod exec -T db sh -c "psql -U \$POSTGRES_USER -d \$POSTGRES_DB -Atc \"$1\""
}
while true; do
  echo "=== $(date +%H:%M:%S) load: $(cut -d' ' -f1-3 /proc/loadavg) mem_avail_MB: $(free -m | awk '/Mem:/{print $7}') swap_used_MB: $(free -m | awk '/Swap:/{print $3}')"
  docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} mem={{.MemUsage}}' | sed 's/holdmytrack-//'
  psql "select 'jobs ' || coalesce(string_agg(kind || '/' || state || '=' || n, ' '), 'none') from (select kind, state, count(*) n from jobs where state <> 'done' group by 1, 2) q"
  psql "select 'db ' || pg_size_pretty(pg_database_size(current_database())) || ' conns=' || (select count(*) from pg_stat_activity)"
  dmesg 2>/dev/null | grep -i "killed process" | tail -1
  sleep 5
done
