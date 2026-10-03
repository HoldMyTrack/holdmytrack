# ADR-0030: Monitoring runs on Grafana Cloud's free tier, fed by one Alloy collector on the server, with personal data scrubbed before anything leaves

## Status

Accepted. Built: `internal/metrics`, `ops/alloy/config.alloy` and `compose.prod.yml`'s `alloy` service (`IMPLEMENTATION.md` §5.8, `docs/DEPLOY.md` §13). Dashboards and alert rules are still to come (`docs/ROADMAP.md`).

## Context

`holdmytrack.com` runs on one droplet, operated by one person. Until now the only way to see what it was doing was `ssh` and `docker compose logs`, and nothing raised an alarm on its own: not a stuck job queue, not a run of `500`s, not a full disk, not Postgres being killed for memory. `scripts/monitor.sh` was a first step, a five-minute cron job reporting to healthchecks.io, but it can't show history or search logs, and its alerts never arrived because healthchecks.io's email didn't reach the inbox.

What's needed is searchable logs, metrics with history (queue size and age, errors by route, job failures by cause, memory, disk), and alerts on them, for no money, on a 4 GB box whose memory belongs to Postgres first.

## Decision

**Hosted on Grafana Cloud's free tier, in a US region beside the server.** Grafana handles dashboards and alerting, Loki stores the logs and Prometheus the metrics. The free tier covers this deployment several times over: logs are kept 14 days, and the stack sends about 1,400 metric series against a 10,000-series limit. When EU hosting comes (Phase 6), an EU deployment gets its own stack in an EU region.

**One collector, Grafana Alloy, as a Compose service.** It tails every container's log through the Docker socket and sends it to Loki. It scrapes the app's own metrics, the host's (CPU, memory, swap, disk, OOM kills) and Postgres's, and sends them to Prometheus. It also picks up a metrics file the backup scripts write, so backup freshness is a metric like any other. It sits under a `monitoring` profile, so a deployment without a Grafana account runs without it.

**The app exposes Prometheus metrics on a listener of its own.** `serve` and `work` answer `GET /metrics` on `METRICS_ADDR`, never on the API's port, since Caddy forwards every unknown path there. The API counts responses by route pattern and status class, times them, and recovers and counts handler panics. The worker counts finished jobs by kind, outcome and failure code, so a failure caused by the user's file can be told from one caused by us. `serve` reads the queue's size and oldest age from the database on each scrape, so those numbers keep coming while the worker is down.

**Personal data is scrubbed in the collector, before it leaves the server.** The app's logs carry ids, job kinds and errors, which may go. Three sources can carry more, and Alloy handles each: `internal/mail`'s lines, which name the recipient, are dropped; Postgres's `DETAIL` lines, which quote constraint values such as an email address, are dropped; and every IP address in Caddy's logs is replaced with `redacted`. No request log is kept at all, so no IP address or URL is shipped from the app.

## Alternatives considered

- **Self-hosted Grafana, Loki and Prometheus on the droplet.** Rejected: together they'd take about 1 GB of the box's 4 GB, and monitoring that runs on the box it watches can't report the box going down.
- **Self-hosted on a second droplet.** Rejected for now: $6–12 a month and a second machine to patch and back up, for what the free tier gives.
- **Better Stack.** Good log search and uptime checks, but weaker metrics, and queue size and job outcomes are what matter most here.
- **Sentry.** Excellent for errors with stack traces, but errors only. Another tool would still be needed for everything else.
- **Keep `scripts/monitor.sh` and healthchecks.io.** Kept as a fallback, not the answer: no history, no search, and alerting depended on email that didn't arrive.
- **Ship every log line unfiltered.** Rejected: an email address in a Postgres `DETAIL` line or an IP address in Caddy's log would sit with a third party for 14 days, for nothing an alert needs.

## Consequences

- Logs and metrics are stored with Grafana Labs, a third party in the US. They hold no tracks, places, names or emails, but they do hold account ids, so the privacy policy (`docs/ROADMAP.md`, Phase 6) must name Grafana Labs as a processor.
- Alloy reads the Docker socket, which makes it as trusted as root on the server. It runs a pinned image, and its own UI listens on its loopback only.
- Free-tier limits bound what can be added: 14 days of logs, and series counted per label combination. That's why metric labels only ever take values from a fixed set (route patterns, job kinds, failure codes), never an id, and why Postgres's per-table statistics are switched off.
- Moving away later means changing where Alloy sends data, not the app: the metrics are Prometheus format, and the logs are the containers' own.
- Scrubbing is a list of known leaks. A new log line carrying personal data would be shipped as it is, so code review has to keep the rule the audit applied: ids, kinds and errors, nothing else.