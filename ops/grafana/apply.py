#!/usr/bin/env python3
"""Push HoldMyTrack's alert rules and dashboard to a Grafana stack (docs/DEPLOY.md §13).

The rules below and dashboard.json beside this file are the source of truth: change them
here and run this again, rather than editing them in Grafana's UI. Run from anywhere with
Python 3 and no other packages:

    GRAFANA_URL=https://<stack>.grafana.net GRAFANA_TOKEN=<service account token> \\
        ops/grafana/apply.py

GRAFANA_TOKEN is a token for a service account with the Admin role, which the alerting
provisioning API needs. DEPLOYMENT is the `deployment` label Alloy adds (DOMAIN in .env.prod),
holdmytrack.com unless set. Rules notify through the stack's default notification policy, so
whatever contact point that uses is where alerts go.
"""

import json
import os
import pathlib
import sys
import urllib.error
import urllib.request

FOLDER_UID = "holdmytrack"
FOLDER_TITLE = "HoldMyTrack"
GROUP = "holdmytrack"
DEPLOYMENT = os.environ.get("DEPLOYMENT", "holdmytrack.com")
D = f'deployment="{DEPLOYMENT}"'

# What the backup ages read as when a script has never recorded a success: an age far past
# any threshold, so the rule fires through its normal path. Grafana doesn't expand a
# summary's template for a no-data alert, so leaving it to noDataState would mail the raw
# template.
NEVER = 1e10

# One alert per row: a PromQL expression evaluated every minute, firing when its value is
# above (">") or below ("<") the threshold for the whole `for` time. Each series the expression
# returns is an alert of its own, so `by (kind)` alerts per job kind. `nodata` is what an empty
# result means: "OK" where nothing to report is the healthy state, "Alerting" where the series
# should always exist and its absence means the data stopped coming.
RULES = [
    dict(uid="hmt-api-5xx", title="API answered with 5xx",
         expr=f'sum(increase(holdmytrack_http_requests_total{{{D}, class="5xx"}}[10m]))',
         op=">", threshold=0, for_="0m", nodata="OK",
         summary="The API answered {{ $values.A.Value }} requests with a 5xx in the last 10 minutes. Logs: {service=\"api\", level=\"ERROR\"}."),
    dict(uid="hmt-jobs-internal", title="Jobs failed on our side",
         expr=f'sum by (kind) (increase(holdmytrack_jobs_finished_total{{{D}, outcome="failed", code="internal"}}[10m]))',
         op=">", threshold=0, for_="0m", nodata="OK",
         summary="{{ $values.A.Value }} {{ $labels.kind }} jobs failed for a reason other than the user's file in the last 10 minutes. Logs: {service=\"worker\"} |= \"job failed\"."),
    dict(uid="hmt-queue-stuck", title="Job queue stuck",
         expr=f'max by (kind) (holdmytrack_jobs_oldest_runnable_age_seconds{{{D}}})',
         op=">", threshold=1800, for_="5m", nodata="OK",
         summary="The oldest runnable {{ $labels.kind }} job has waited {{ humanizeDuration $values.A.Value }}. Is the worker running?"),
    dict(uid="hmt-app-down", title="api or worker not answering",
         expr=f'max by (service) (up{{{D}, job="prometheus.scrape.app"}})',
         op="<", threshold=1, for_="5m", nodata="OK",
         summary="{{ $labels.service }} hasn't answered a metrics scrape for 5 minutes."),
    dict(uid="hmt-postgres-down", title="Postgres down",
         expr=f'max(pg_up{{{D}}})',
         op="<", threshold=1, for_="2m", nodata="Alerting",
         summary="The Postgres exporter can't reach the database."),
    dict(uid="hmt-monitoring-silent", title="No metrics arriving",
         expr=f'count(up{{{D}}})',
         op="<", threshold=1, for_="10m", nodata="Alerting",
         summary="Nothing has arrived from the server for 10 minutes: the droplet, Docker or the alloy service is down."),
    dict(uid="hmt-disk", title="Disk over 85% full",
         expr=f'max(1 - node_filesystem_avail_bytes{{{D}, fstype=~"ext4|xfs|btrfs"}} / node_filesystem_size_bytes{{{D}, fstype=~"ext4|xfs|btrfs"}})',
         op=">", threshold=0.85, for_="15m", nodata="OK",
         summary="The disk is {{ humanizePercentage $values.A.Value }} full."),
    dict(uid="hmt-memory", title="Memory nearly exhausted",
         expr=f'min(node_memory_MemAvailable_bytes{{{D}}} / node_memory_MemTotal_bytes{{{D}}})',
         op="<", threshold=0.1, for_="10m", nodata="OK",
         summary="Only {{ humanizePercentage $values.A.Value }} of memory is available."),
    dict(uid="hmt-oom", title="Process killed for lack of memory",
         expr=f'sum(increase(node_vmstat_oom_kill{{{D}}}[10m]))',
         op=">", threshold=0, for_="0m", nodata="OK",
         summary="The kernel's OOM killer killed {{ $values.A.Value }} processes in the last 10 minutes. Check that db, api and worker are running."),
    dict(uid="hmt-backup-stale", title="Backup overdue",
         expr=f'(time() - max(holdmytrack_backup_last_success_timestamp_seconds{{{D}}})) or vector({NEVER})',
         op=">", threshold=26 * 3600, for_="10m", nodata="Alerting",
         summary="{{ if ge $values.A.Value 1e9 }}There's no record of a successful backup.sh run{{ else }}The last successful backup.sh run was {{ humanizeDuration $values.A.Value }} ago{{ end }}. See /var/log/holdmytrack-backup.log."),
    dict(uid="hmt-drill-stale", title="Restore drill overdue",
         expr=f'(time() - max(holdmytrack_restore_drill_last_success_timestamp_seconds{{{D}}})) or vector({NEVER})',
         op=">", threshold=33 * 86400, for_="1h", nodata="Alerting",
         summary="{{ if ge $values.A.Value 1e9 }}There's no record of a successful restore-drill.sh run{{ else }}The last successful restore-drill.sh run was {{ humanizeDuration $values.A.Value }} ago{{ end }}. See /var/log/holdmytrack-backup.log."),
]


def api(method, path, body=None, headers=None):
    req = urllib.request.Request(
        os.environ["GRAFANA_URL"].rstrip("/") + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={
            "Authorization": "Bearer " + os.environ["GRAFANA_TOKEN"],
            "Content-Type": "application/json",
            **(headers or {}),
        },
    )
    try:
        with urllib.request.urlopen(req) as resp:
            text = resp.read().decode()
            return json.loads(text) if text else None
    except urllib.error.HTTPError as err:
        sys.exit(f"{method} {path}: {err.code} {err.read().decode()[:500]}")


def datasource(kind, suffix):
    """The stack's own Prometheus or Loki: on Grafana Cloud, grafanacloud-<stack>-prom and
    -logs, beside others of the same type (usage, alert state history) to pass over."""
    env = os.environ.get(f"GRAFANA_{kind.upper()}_UID")
    if env:
        return env
    found = [d for d in api("GET", "/api/datasources") if d["type"] == kind and d["name"].endswith(suffix)]
    if len(found) != 1:
        names = [d["name"] for d in found]
        sys.exit(f"expected one {kind} data source named *{suffix}, found {names}; set GRAFANA_{kind.upper()}_UID")
    return found[0]["uid"]


def rule(r, prom):
    return {
        "uid": r["uid"],
        "title": r["title"],
        "folderUID": FOLDER_UID,
        "ruleGroup": GROUP,
        "condition": "C",
        "for": r["for_"],
        "noDataState": r["nodata"],
        "execErrState": "Error",
        "annotations": {"summary": r["summary"]},
        "labels": {"deployment": DEPLOYMENT},
        "data": [
            {
                "refId": "A",
                "relativeTimeRange": {"from": 600, "to": 0},
                "datasourceUid": prom,
                "model": {"refId": "A", "expr": r["expr"], "instant": True, "range": False},
            },
            {
                "refId": "C",
                "relativeTimeRange": {"from": 0, "to": 0},
                "datasourceUid": "__expr__",
                "model": {
                    "refId": "C",
                    "type": "threshold",
                    "expression": "A",
                    "conditions": [{"evaluator": {"type": "gt" if r["op"] == ">" else "lt", "params": [r["threshold"]]}}],
                },
            },
        ],
    }


def main():
    for var in ("GRAFANA_URL", "GRAFANA_TOKEN"):
        if not os.environ.get(var):
            sys.exit(f"{var} is not set; see this file's docstring")
    prom = datasource("prometheus", "-prom")
    loki = datasource("loki", "-logs")

    if not any(f["uid"] == FOLDER_UID for f in api("GET", "/api/folders")):
        api("POST", "/api/folders", {"uid": FOLDER_UID, "title": FOLDER_TITLE})

    # The whole group at once: a rule removed from RULES is removed from Grafana too.
    # X-Disable-Provenance leaves the rules editable in the UI, for trying a change out
    # before making it here.
    api("PUT", f"/api/v1/provisioning/folder/{FOLDER_UID}/rule-groups/{GROUP}",
        {"title": GROUP, "folderUid": FOLDER_UID, "interval": 60, "rules": [rule(r, prom) for r in RULES]},
        headers={"X-Disable-Provenance": "true"})
    print(f"alert rules: {len(RULES)} in {FOLDER_TITLE}/{GROUP}")

    board = json.loads(pathlib.Path(__file__).with_name("dashboard.json").read_text())
    text = json.dumps(board).replace("${PROM}", prom).replace("${LOKI}", loki).replace("${DEPLOYMENT}", DEPLOYMENT)
    result = api("POST", "/api/dashboards/db", {"dashboard": json.loads(text), "folderUid": FOLDER_UID, "overwrite": True})
    print(f"dashboard: {os.environ['GRAFANA_URL'].rstrip('/')}{result['url']}")


if __name__ == "__main__":
    main()
