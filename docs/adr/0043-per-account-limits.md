# ADR-0043: Every account can keep up to 50,000 activities and 5 GB of original files, the same limits for all

## Status

Accepted. Built: the server checks them on upload, phone sync and accepting a Story copy (`SPEC.md` FR-1.13), and Settings shows what an account holds against them.

## Context

Storage growing without bound is the first thing `VISION.md` §4.3 names as breaking a free service, and the rate limits (`IMPLEMENTATION.md` §5.7) only bound how fast an account adds: 100 uploads an hour, 60 sync requests an hour, 16 MiB a file. A script staying under them adds thousands of activities a day, every day, and each one keeps costing: its original upload in R2, its masks in Postgres on the droplet's disk, its share of the Fog and Heatmap tiles, and the renders behind them. Photos were the one stored line with a ceiling (2,000 per account, ADR-0024).

Against that, `VISION.md` §1 promises to keep the journeys people choose, and ADR-0031 rules out tiers: every account has every feature.

## Decision

- **Two limits per account**: 50,000 activities, and 5 GB (decimal) of original files, the uploads and syncs as they arrived. The count is what a person can picture; the bytes catch a few very large files that the count alone wouldn't. Masks and tiles grow with activities and with ground covered, so the two bound them too.
- **Far above any person's use.** 50,000 is about nine activities a day for fifteen years; 5 GB is about 60,000 activities at the ~80 KB `VISION.md` §4.3 assumes. They are there to stop a script or a runaway import, not to ration anyone.
- **The same for every account, with no per-account raise.** Raising one account's limit would be a tier. If a real person nears them, the limits go up for everyone.
- **Only adding stops.** At a limit, an upload, a phone sync and accepting a Story copy are refused with a message naming the limit; everything else keeps working — viewing, editing, deleting, Stories, downloading the data. Nothing is ever deleted or hidden to make room, and deleting activities frees it.
- **Waiting imports count**, so a large batch can't slip past while it's queued. The check is made before the jobs are queued, without a lock: two requests at once can together go a little over, which limits this far above use can afford.
- **A Story copy accepted below the limits is made whole**, even if it ends past them: its activities are the recipient's choice once accepted, and half a Story would be worse than a little over.
- **Settings shows the account's use of both**, so nobody learns of the limits only by meeting them.
- **The demo account is exempt**, having no way to add anything.

## Alternatives considered

- **No limits, only rate limits and monitoring.** Leaves the bill open-ended between someone noticing and doing something, and the only thing to do then is act on one account by hand.
- **A limit on stored points.** Per-point streams aren't stored any more; what costs is files, masks and tiles, which activities and bytes measure directly.
- **One combined storage figure (originals, masks, tiles, photos).** Closer to the bill, but no one can predict their tile and mask bytes, so the limit would be met without warning and couldn't be explained. Photos keep their own cap.
- **Limits set from today's accounts' p90.** Would catch real people. The daily statistics (`IMPLEMENTATION.md` §4.32) are for watching how close the heaviest account gets, not for setting the line under them.
- **Hiding or archiving the oldest activities past the limit.** Breaks the promise that nothing is changed on the user's behalf (ADR-0039).

## Consequences

- **One account's cost has a ceiling.** At the limits, about 5 GB of R2 and on the order of 250 MB of masks in Postgres.
- **A real user could one day meet them**, and then the only fix is raising them for everyone; lowering them later would strand accounts above the new line, so they only go up.
- **The limit is a promise apps see**: the clients show the server's message as it is, so a change of limit needs no app release, but the error's shape does.
- **Racing requests can overshoot slightly**, and a Story copy can carry an account past a limit. Neither changes what the limits are for.
- **The heaviest account is watched**: the daily snapshot's `activities_per_account.max` and `storage_bytes_per_account.max` feed an alert at half a limit, so a real person nearing one is noticed long before they reach it.