---
name: deploy
description: Deploy HoldMyTrack's current origin/main to production (holdmytrack.com) — build with the site up, backup when there are migrations and maintenance mode only for one that needs it, verify, and republish the Android APK when the app changed. Use only when the user asks to deploy.
disable-model-invocation: true
---

# Deploy HoldMyTrack to production

Production is holdmytrack.com: one droplet, reached as `ssh holdmytrack` (root), repo checked out at `/srv/holdmytrack` tracking `main`, Compose project `holdmytrack`, config in `.env.prod`. `holdmytrack` is an alias in the operator's own `~/.ssh/config`, so the server's address isn't written here. `docs/DEPLOY.md` is the authority (§6 bring it up, §7 maintenance mode, §8 verify, §10 APK); this skill is the routine path through it.

Every compose call on the server is `docker compose -f compose.prod.yml --env-file .env.prod …` — never drop `--env-file`. `.env.prod` can't be shell-sourced (unquoted multi-word values), so read DB credentials inside the `db` container.

## Who runs the SSH commands

Auto mode usually blocks Claude's SSH to production. Try once; if it's denied, don't work around it — hand each step to the user as a `! ssh holdmytrack '…'` line (the `!` runs it in the session so its output lands in the conversation), one step per message, and read the output before giving the next.

## 1. Plan locally (no SSH)

```
git fetch -q origin
DEPLOYED=$(curl -s https://holdmytrack.com/healthz | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
git log --oneline $DEPLOYED..origin/main
git diff --stat $DEPLOYED origin/main -- services/server/migrations apps/android
git diff $DEPLOYED origin/main -- services/server/migrations | grep -E '^\+\+\+ |^\+-- Deploy: maintenance'
git diff $DEPLOYED origin/main -- services/server/internal/geo | grep -E '^[-+].*(BoundariesKey|TimezonesKey) *='
git diff --stat $DEPLOYED origin/main -- services/server/internal/fog services/server/internal/tilemath services/server/internal/httpapi/demo_data
git diff $DEPLOYED origin/main -- docs/DEPLOY.md
```

Tell the user: what's deployed, the target `origin/main` short SHA, the commits in between, and whether there are **new migrations** (files under `services/server/migrations/`) and whether any **needs maintenance mode** (its first line is `-- Deploy: maintenance — <why>`, `docs/DEVELOPMENT.md`'s "Writing a migration"), **Android changes** (anything under `apps/android/` outside `docs/`) and **one-time server steps** (step 3's last part). Read every new migration without that line too, and if one looks unsafe to run beside the old code (a drop, a rename, a new `NOT NULL`, a long rewrite or backfill), say why and ask whether to deploy it with maintenance mode instead. Read the `DEPLOY.md` diff whole: a feature that needs an operator step (a file to load, a command to run once) adds it there, and nothing else in this plan would show it. Only `origin/main` deploys — unmerged branches never do. If `/healthz` doesn't answer, say so and ask before going on. Confirm with the user before starting; a deploy changes the live site.

### Android version bump (only if the app changed)

`versionCode` is the commit count and takes care of itself; `versionName` is the release number, bumped by hand when a release means something (`apps/android/docs/IMPLEMENTATION.md` §8). Show the user where it stands:

```
git show origin/main:apps/android/holdmytrack/app/build.gradle.kts | grep 'versionName ='
BUMPED=$(git log -1 --format=%h -G'versionName = ' origin/main -- apps/android/holdmytrack/app/build.gradle.kts)
git log --oneline --no-merges $BUMPED..origin/main -- apps/android ':!apps/android/docs'
```

Give the current `versionName`, the commit that last set it, and the app's commits since — summarised as features, not a raw list — and ask whether this release deserves a bump, suggesting the next number (0.4 → 0.5) and a one-word theme when the features have one. Ask together with the deploy confirmation, so it's one question.

The APK reads `versionName` from the commit it's built from, so a bump has to be on `origin/main` before the deploy. If the user wants one, before step 2:

1. Branch off `origin/main` (`android-version-0.5`) and change the number everywhere it's written: `versionName` in `apps/android/holdmytrack/app/build.gradle.kts`, and the examples of it in `apps/android/docs/IMPLEMENTATION.md` §8 ("The version comes from git"), `apps/android/docs/SPEC.md` FR-2.9 ("Version 0.4 (62da50b)") and the KDoc of `appVersion` and `UserAgentInterceptor` in `net/HoldMyTrackApi.kt`. `git grep -n '0\.4' apps/android` finds them; leave the unrelated matches (distances like "10.4 km", opacities).
2. One commit, `Android: version 0.5 — <theme>`, with a body naming what the release brings; push it and give the user the compare URL to open the PR (`gh` may not be installed).
3. Wait for the user to say it's merged, then re-run the plan above: the target SHA is now the merge commit.

No bump: go on with the target as it is.

## 2. Deploy

Build first in every case (after the backup, when there are migrations): `git pull` and `build` leave the running containers alone (none of them mounts the source), so the old version keeps serving through the slow part. The build outlasts the foreground timeout, so if Claude runs it, use `run_in_background`.

```
! ssh holdmytrack 'cd /srv/holdmytrack && git pull --ff-only && GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod build'
```

**Without new migrations** — then swap the containers:

```
! ssh holdmytrack 'cd /srv/holdmytrack && GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d'
```

**With new migrations, none needing maintenance** — DEPLOY.md §7, the site stays up throughout:

1. Backup (also copies to the backup R2 bucket): `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/backup.sh'` — before the build, and it must finish without error before anything else.
2. Pull and build, as above.
3. Migrate beside the running release: `! ssh holdmytrack 'cd /srv/holdmytrack && docker compose -f compose.prod.yml --env-file .env.prod run --rm migrate'` — never fold this into `up -d`, which takes `api` down for as long as `migrate` runs.
4. Swap, as in the case without migrations.

**With a migration that needs maintenance mode** — DEPLOY.md §7, maintenance only around the swap:

1. Backup, as above.
2. Pull and build, as above — the site stays up.
3. Maintenance on, swap: `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/maintenance.sh on && GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d'`
4. Check (step 3 below) — `migrate` must be `Exited (0)` and `/healthz` 200 before going on.
5. Maintenance off: `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/maintenance.sh off'`

If `git pull --ff-only` refuses, the server checkout has drifted: stop and show the user `git status`, don't reset it. If `run --rm migrate` fails, nothing has been swapped: show its output and stop, leaving the old release running. If `migrate` exited non-zero under maintenance, leave maintenance on, show its logs (`… logs migrate`), and stop — the backup from step 1 is the way back (DEPLOY.md §11).

## 3. Verify

```
! ssh holdmytrack 'cd /srv/holdmytrack && docker compose -f compose.prod.yml --env-file .env.prod ps -a && curl -s https://holdmytrack.com/healthz'
```

`/healthz`'s `version` must equal the target short SHA; `migrate` is `Exited (0)`; `api`, `worker`, `web`, `db` are up. Then, from the Mac (no SSH needed), curl the public pages the deployed commits touched and confirm they return 200 — e.g. a new page, `/sitemap.xml` listing it.

Then the one-time steps the plan found, each from DEPLOY.md §6, after maintenance is off unless §6 says otherwise. Each writes to the production database, so list them in the step 1 confirmation and ask before running them:

- **A new or changed `geo.TimezonesKey`**: download that release's `timezones-with-oceans.geojson.zip` to `/tmp` on the server, `rclone copyto` it into the app bucket under the key, run `run --rm api seed-timezones`, then delete the `/tmp` copy. Without it `tz_parts` stays empty and every activity keeps its account's zone, which nothing else reports: check `select count(*) from tz_parts` is non-zero afterwards.
- **A new or changed `geo.BoundariesKey`**: the file is made off the server (`scripts/boundaries-extract.sh`), so ask the user for it, copy it into the bucket the same way, and run `seed-admin-boundaries`, behind a backup and maintenance mode (§6).
- **Changed `demo_data/`**: `run --rm api seed-demo-customer --reset`.
- **Changed `internal/fog` or `internal/tilemath`**: `run --rm api rerender-coverage`, with `--masks` when the stroke or which tiles a track reaches changed (§6). Say which commits touched it and what they change, so the user can decide whether existing tiles look different.

## 4. Android APK (only if the app changed)

From the deployed commit, so the APK's SHA matches `/healthz`:

```
cd apps/android/holdmytrack    # not apps/android
./gradlew :app:assembleDebug -Pholdmytrack.apiBaseUrl=https://holdmytrack.com
! scp app/build/outputs/apk/debug/app-debug.apk holdmytrack:/srv/holdmytrack/downloads/holdmytrack.apk
```

Check `curl -sI https://holdmytrack.com/download/holdmytrack.apk` — singular `/download/`; the server directory is plural `downloads/`, and `/downloads/…` is a misleading 404.

## 5. Report and record

Tell the user the deployed SHA, whether migrations ran (and the backup's name, and whether the site went into maintenance mode), which one-time steps ran or were skipped and why, whether the APK was republished and at which `versionName`, and what was verified. If you keep notes on the deployment's state between sessions, record the deployed SHA there.