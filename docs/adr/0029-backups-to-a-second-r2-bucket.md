# ADR-0029: Nightly backups go to a second R2 bucket, and only what can't be rebuilt is copied

## Status

Accepted. Built: `scripts/backup.sh`, `scripts/restore-drill.sh` and `compose.prod.yml`'s `rclone` service (`docs/DEPLOY.md` §11, `IMPLEMENTATION.md` §5.8).

## Context

`holdmytrack.com` holds real people's history: synced Health Connect activities, Takeout imports, photos. All of it lives in two places, the `db` container's `pgdata` volume on one VPS and the app's R2 bucket, and neither had a second copy. A dropped volume, a bad migration or a mistaken `rclone`/`aws s3` command would lose it for good, and with no restore path every deploy carried that risk too, which is why CI doesn't deploy on merge.

A backup that has never been restored can't be trusted. Dumps can be cut short, an object sync can quietly skip a prefix, and a database can refer to objects that never made it into the copy. Proving this needs a regular drill that checks the restored database against the restored objects, not just a dump that exists.

The deployment is one small VPS with no budget beyond what donations will cover, run by one person.

## Decision

**A nightly script on the VPS, run by cron, writes to a second, private R2 bucket with its own token.** `backup.sh` takes a `pg_dump -Fc` of the live database and checks that `pg_restore` can read it. It keeps the dump on the host for a week and uploads it to the bucket's `postgres/daily/`, kept 14 days, plus Sunday's to `postgres/weekly/`, kept 8 weeks. It then syncs the app bucket's `raw/`, `photos/` and `avatars/` into `objects/`. Anything the sync would delete or overwrite moves into `objects-deleted/<run>/` instead, kept 30 days, so a mistake in the app bucket can still be undone a month later. The backup token is scoped to the backup bucket alone, so a leaked app token can't touch the backups.

**`fog/`, `heatmap/` and `activity-masks/` aren't backed up.** They make up most of the app bucket's objects and churn with every edit, and `rerender-coverage --masks` rebuilds all three from the database and `raw/`.

**rclone runs from its own image as a Compose service under an `ops` profile.** Nothing gets installed on the host, the image version is pinned in the repo, and the credentials come from `.env.prod` through Compose's own parsing, so they never appear on a command line.

**A monthly drill restores the newest dump into a throwaway container and checks the objects against it.** `restore-drill.sh` fails when the newest dump is more than 36 hours old, when `pg_restore` fails, when the restored database has no users, or when any object it refers to (a raw payload, a photo and its thumbnail, an avatar) is missing from both `objects/` and `objects-deleted/`. Each script pings an optional heartbeat URL only on success, so a run that stops running is noticed as well as one that fails.

## Alternatives considered

- **Backblaze B2 or DigitalOcean Spaces as the destination.** These are independent of Cloudflare, so a lost Cloudflare account or an R2 outage wouldn't take the backups along with the app bucket. Not chosen, for now: R2 costs nothing at this size, server-side copies are fast, and a separate bucket and token already guard against the likelier losses (a bad deploy, a wrong command, a leaked app token). Moving later means changing `BACKUP_S3_ENDPOINT` and the keys in `.env.prod`; the scripts are the same.
- **DigitalOcean's droplet backups or volume snapshots.** Rejected as the backup. A snapshot of a running Postgres is only crash-consistent, it doesn't cover R2, it lives with the same provider and account as the server, and it can't be checked by restoring it beside the live database. Droplet backups are still a cheap extra.
- **Continuous archiving (WAL-G or pgBackRest) for point-in-time recovery.** Rejected for now. Losing up to a day of changes is acceptable while the site is a sandbox, and continuous archiving would add a long-running process, more retention to manage and more to drill on a 2 GB box. Worth revisiting before Production.
- **Back up the whole app bucket.** Rejected: it would mostly copy tiles that can be rebuilt, and pay R2's per-operation charges for them every night.
- **Plain `rclone sync` with no `--backup-dir`.** Rejected: a deletion or overwrite in the app bucket would reach the backup the next night, so the backup would only protect against losing the bucket outright, not against a bad command.

## Consequences

- An account deleted on request stays in the backups for up to 8 weeks (the weekly dumps, and 30 days for its objects in `objects-deleted/`). That needs saying in the privacy policy before a public launch.
- The backups are only as safe as the Cloudflare account. A lost account loses both copies.
- Recovery loses up to a day of changes: whatever was uploaded, synced or edited since the last dump.
- A restore needs `rerender-coverage --masks` once the database is back, and the map shows no Fog or Heatmap until that finishes.
- The drill runs a second Postgres on a 2 GB box for a few minutes each month, so cron runs it at night.