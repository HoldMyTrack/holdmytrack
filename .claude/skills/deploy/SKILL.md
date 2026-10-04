---
name: deploy
description: Deploy HoldMyTrack's current origin/main to production (holdmytrack.com) — backup and maintenance mode when there are migrations, rebuild, verify, and republish the Android APK when the app changed. Use only when the user asks to deploy.
disable-model-invocation: true
---

# Deploy HoldMyTrack to production

Production is holdmytrack.com: one droplet, reached as `ssh holdmytrack` (root), repo checked out at `/srv/holdmytrack` tracking `main`, Compose project `holdmytrack`, config in `.env.prod`. `holdmytrack` is an alias in the operator's own `~/.ssh/config`, so the server's address isn't written here. `docs/DEPLOY.md` is the authority (§6 bring it up, §7 maintenance mode, §8 verify, §10 APK); this skill is the routine path through it.

Every compose call on the server is `docker compose -f compose.prod.yml --env-file .env.prod …` — never drop `--env-file`. `.env.prod` can't be shell-sourced (unquoted multi-word values), so read DB credentials inside the `db` container.

## Who runs the SSH commands

Auto mode usually blocks Claude's SSH to production. Try once; if it's denied, don't work around it — hand each step to the user as a `! ssh holdmytrack '…'` line (the `!` runs it in the session so its output lands in the conversation), one step per message, and read the output before giving the next. The build outlasts the 120 s foreground timeout, so if Claude runs it, use `run_in_background`.

## 1. Plan locally (no SSH)

```
git fetch -q origin
DEPLOYED=$(curl -s https://holdmytrack.com/healthz | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
git log --oneline $DEPLOYED..origin/main
git diff --stat $DEPLOYED origin/main -- services/server/migrations apps/android
```

Tell the user: what's deployed, the target `origin/main` short SHA, the commits in between, and whether there are **new migrations** (files under `services/server/migrations/`) and **Android changes** (anything under `apps/android/` outside `docs/`). Only `origin/main` deploys — unmerged branches never do. If `/healthz` doesn't answer, say so and ask before going on. Confirm with the user before starting; a deploy changes the live site.

## 2. Deploy

**Without new migrations** — one step:

```
! ssh holdmytrack 'cd /srv/holdmytrack && git pull --ff-only && GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d --build'
```

**With new migrations** — DEPLOY.md §7, in order:

1. Backup (also copies to the backup R2 bucket): `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/backup.sh'` — it must finish without error before anything else.
2. Maintenance on, pull, rebuild: `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/maintenance.sh on && git pull --ff-only && GIT_SHA=$(git rev-parse --short HEAD) docker compose -f compose.prod.yml --env-file .env.prod up -d --build'`
3. Check (step 3 below) — `migrate` must be `Exited (0)` and `/healthz` 200 before going on.
4. Maintenance off: `! ssh holdmytrack 'cd /srv/holdmytrack && ./scripts/maintenance.sh off'`

If `git pull --ff-only` refuses, the server checkout has drifted: stop and show the user `git status`, don't reset it. If `migrate` exited non-zero, leave maintenance on, show its logs (`… logs migrate`), and stop — the backup from step 1 is the way back (DEPLOY.md §11).

## 3. Verify

```
! ssh holdmytrack 'cd /srv/holdmytrack && docker compose -f compose.prod.yml --env-file .env.prod ps -a && curl -s https://holdmytrack.com/healthz'
```

`/healthz`'s `version` must equal the target short SHA; `migrate` is `Exited (0)`; `api`, `worker`, `web`, `db` are up. Then, from the Mac (no SSH needed), curl the public pages the deployed commits touched and confirm they return 200 — e.g. a new page, `/sitemap.xml` listing it.

A change to Fog/Heatmap drawing (`services/server/internal/fog`) also needs `run --rm api rerender-coverage` (DEPLOY.md §6) — call it out if the commits touch it, and ask first.

## 4. Android APK (only if the app changed)

From the deployed commit, so the APK's SHA matches `/healthz`:

```
cd apps/android/holdmytrack    # not apps/android
./gradlew :app:assembleDebug -Pholdmytrack.apiBaseUrl=https://holdmytrack.com
! scp app/build/outputs/apk/debug/app-debug.apk holdmytrack:/srv/holdmytrack/downloads/holdmytrack.apk
```

Check `curl -sI https://holdmytrack.com/download/holdmytrack.apk` — singular `/download/`; the server directory is plural `downloads/`, and `/downloads/…` is a misleading 404.

## 5. Report and record

Tell the user the deployed SHA, whether migrations ran (and the backup's name), whether the APK was republished, and what was verified. If you keep notes on the deployment's state between sessions, record the deployed SHA there.