# Performance

How to run a load test or benchmark, then the latest result per area and the finding that shaped the design. How each measured piece works lives in `IMPLEMENTATION.md`. Earlier sessions are in git history; a finding from one stays as a dated line under its area where another document cites it.

## Running a session

### Tools

- `services/server/cmd/loadtest`: the driver. Its package comment lists the subcommands and flags. The target is `-base`; tokens and track sets go in `-dir`, outside the repo.
- `scripts/loadtest-sampler.sh`: run on the server during a test. Every 5 s it records host load and memory, each container's CPU and memory, the job queue by kind and state, and database size and connections.
- `services/server/cmd/delayproxy`: adds latency in front of the dev stack's object store, so a render on a laptop pays per object what production pays to R2. Sessions below use 80 ms a round trip.
- `go test -bench` in `services/server/internal/fog`: per-tile costs (`tilecost_bench_test.go`).

### Test accounts

Production requires a verified email (`SPEC.md` FR-1.8), so test accounts are created while `api` runs with `SKIP_EMAIL_VERIFICATION=true` from a throwaway Compose override; `compose.prod.yml` doesn't pass that variable through.

1. On the server, write `/tmp/lt-override.yml` adding `SKIP_EMAIL_VERIFICATION: "true"` and `SIGNUPS_PER_HOUR: "100"` to `api`'s environment, and recreate `api` with `-f compose.prod.yml -f /tmp/lt-override.yml … up -d --no-deps api`.
2. `loadtest signup 21` (20 load accounts, the last one for `probe`).
3. Recreate `api` without the override straight away and delete the file. Check that no account other than the test ones was created while the bypass was on.

For the run, the same kind of override raises three limits, which go back to their defaults afterwards:
- `UPLOADS_PER_HOUR` (say to 100000): `seed` and `import` send far more than an account's 100 files an hour (`SPEC.md` FR-3.1).
- `TILES_PER_MINUTE`: `browse` runs several visitors per account, past one account's 1,200 tiles a minute (`SPEC.md` FR-4.15).
- `R2_DAILY_BUDGET_USD`: the run's R2 requests count toward the daily budget (`DEPLOY.md` §4), whose stages would pause renders and refuse uploads mid-test.

Afterwards `loadtest cleanup` deletes the accounts and the purge (`IMPLEMENTATION.md` §4.28) removes their rows and objects. Check that no test row or pending job is left.

### Scenarios

- **Seed**: `seed` uploads the 45 demo tracks (`internal/httpapi/demo_data`) to every account, about 945 jobs, and the queue is timed to empty. Three tracks cross 200+ z14 tiles each (the "2026-01-22 Vietnam" drive 756).
- **Browse ramp**: `browse` with 10, 25, 50, 100 and 200 people for 3 minutes each, the worker idle. Each person mixes Normal, Fog and Heatmap and every 1.5–4 s pans, zooms or switches mode, requesting what the web client would for a 1280×800 window and keeping fetched tiles per tile version as a browser does (`IMPLEMENTATION.md` §4.2.6). `-cold` drops that cache; `-mode` keeps everyone in one mode. A view costs 2.3–4 requests with the cache, about 7.5 cold.
- **Import**: `gen 5 1000` then `import 5`: five accounts each upload a 1,000-track set at once, one file at a time as the web's Upload menu does. Two minutes in, and again deep into the backlog, `probe` uploads one track from another account and times it to processed, the fairness check. A 12-person `browse` runs meanwhile.
- **Stop** a step on more than 2% errors, a p95 over 5 s on two consecutive steps, under 10% memory available, or an out-of-memory kill.

## Results

### Production load test — 2026-10-10, `535cec9`

The production VPS (2 vCPU, 4 GB) with two render loops beside four main ones (`IMPLEMENTATION.md` §4.1), masks in Postgres (`IMPLEMENTATION.md` §3.11), 128 px tiles, Country and Region outlines kept per tile (`IMPLEMENTATION.md` §4.2.4) and Postgres's cache at 1 GB (`DEPLOY.md` §6). All three scenarios, 21 accounts.

| People | Views/s | Fog p95 | Heatmap p95 | Tracks p95 | Region p95 | Country p95 | Errors |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| 10 | 3.4 | 0.17 s | 0.12 s | 0.07 s | 0.51 s | 0.22 s | 0 |
| 50 | 17.4 | 0.15 s | 0.15 s | 0.09 s | 0.22 s | 0.20 s | 0 |
| 100 | 35.2 | 0.21 s | 0.20 s | 0.13 s | 0.10 s | 0.08 s | 0 |
| 200 | 69.6 | 0.67 s | 0.62 s | 0.51 s | 0.13 s | 0.11 s | 0 |

- Views rose in step with people to 200; nothing saturated. Postgres averaged 39% CPU at 200 people, the API under 50%; at least 2.2 GB of memory stayed free.
- Seed: accepted in 25 s, queue drained in 21 minutes. Import: 5,000 files sent in about 2 minutes without a failure, ingested in 11, renders done 25 minutes later.
- Fairness: a `probe` upload was processed in 5 s both times, once with two renders running and five queued.
- Browsing during the import: every p95 under 374 ms, no failed request.
- The purge removed the 21 accounts in about 9 minutes, leaving no pending job or orphaned tile.

Earlier findings behind current choices:
- 2026-10-08: the API saturated at about 6 views/s decoding, colouring and encoding each Fog and Heatmap tile per request, which led to serving stored bytes. One first-in-first-out worker left another account's upload waiting over 55 minutes, which led to the fair worker (`IMPLEMENTATION.md` §4.1). Renders ran at about 1.3 tiles/s on sequential object-store round trips, which led to running them side by side (`IMPLEMENTATION.md` §4.2).
- 2026-10-09, `c040021`: once tiles were served from stored bytes, Postgres ran out first, at 110–145% CPU on the live Country, Region and Tracks tiles.
- 2026-10-09, `7d013f0`: redrawing 835 activities' masks one at a time took about 0.4 s each, nearly all R2 round trips, which led to masks in Postgres and redraws side by side.
- 2026-10-09, `f3f28d2`: a probe sent while four accounts' renders held all four worker loops waited 7 min 35 s, which led to the render lane.

### Render round trips — 2026-10-10, dev stack

A tile's object-store round trips run side by side (`IMPLEMENTATION.md` §4.2 "Invalidation"): its Fog and Heatmap PUTs together, a pyramid tile's children read in one query and fetched together. With `delayproxy` and two render loops, re-rendering two 1,000-track accounts in full (7,813 and 8,134 z14 tiles plus pyramid) took 152 / 180 s against 365 / 423 s before. Served tiles were byte-identical between builds.
- 2026-10-09: rendering sixteen tiles at a time took the Vietnam track's render from 780 s to 52 s. Redrawing a 1,000-activity account's masks in parallel took 37 s against 311 s, and purging it 36 s against 225 s.
- 2026-10-10: moving masks into Postgres alone made a full render about 10% faster; the tile's own round trips were the rest.

### Tile size — 2026-10-09, dev stack

The choice of 128 px Fog and Heatmap tiles (`IMPLEMENTATION.md` §4.2). One seeded account redrawn at both sizes and screenshotted at zoom 10, 13 and 16.

| Per tile, laptop | 64 px | 128 px |
| :-- | :-- | :-- |
| Composite Fog / Heatmap | 6.8 / 13 µs | 27 / 65 µs |
| A pyramid step | 7.8 µs | 31 µs |
| A stored mask, average | about 275 bytes | 616 bytes |

- At zoom 10 the 64 px tracks merged where they ran close; at 128 px they stayed apart. At zoom 13 the 128 px lines followed their roads where 64 px drew a staircase.
- 300 random jittery walks all drew at 128 px with `minStepPx` at half a pixel; without that skip, GPS jitter while standing still made segments too short to stroke (`IMPLEMENTATION.md` §4.2 "Rendering a tile").
- 512 px composited Fog in 1.1 ms and Heatmap in 3.3 ms and stored about 5 times the bytes of 128 px; 256 px looked the same as 512.

### Country and Region tiles — 2026-10-09, dev stack

What keeping clipped outlines per tile (`admin_tile_geoms`, `IMPLEMENTATION.md` §4.2.4) saves. Queries run in `psql` as an account with 611 activities, third of three runs.

| Tile | Live | Outlines alone | Account filter alone | Built once | Kept |
| :-- | :-- | :-- | :-- | :-- | :-- |
| Country z0/0/0 | 80 ms | 85 ms | 0.8 ms | 191 ms | 2.3 ms |
| Region z4/4/5 | 29 ms | 29 ms | 0.8 ms | 56 ms | 0.9 ms |

- Transforming and clipping the outlines is nearly all of an expensive tile. The account filter starts from the account's own activities, since an `EXISTS` over a shared region scanned every account's matches.
- A kept tile serves the same bytes in 0.7–2.3 ms; its first request costs about twice the live query, once per tile for all accounts.

### Bike-path tiles — 2026-10-10, dev stack

`bikePathsQuery` in `psql` over downtown Cleveland with the Ohio extract (16,208 ways, `IMPLEMENTATION.md` §4.24), warm: at z9 a 13.6 KB tile took 140 ms with lines stored in WGS 84 and reprojected per request, 2.2 ms stored in Web Mercator; at z13, 1.2 ms against 0.5 ms. The table stores Web Mercator. No region denser than Ohio was measured.

### Heatmap as pass counts — 2026-10-15, dev stack

Storing the Heatmap as pass counts drawn as MapLibre heatmap circles (`IMPLEMENTATION.md` §4.2.2, ADR-0046) against the rendered raster, on three accounts, with object-store requests read off `r2_ops`; panning on a Pixel 10a held 60 fps with either.

| Measure | Raster | Counts |
| :-- | :-- | :-- |
| Full re-render, 1,339 z14 tiles | 3.4 / 3.6 s | 3.4 / 3.7 s |
| Five uploads replayed, object-store requests | 316 / 522 | 316 / 520 |
| Stored Heatmap, z14 / zoomed out | 1,041 / 753 KB | 679 / 634 KB |

- Counting a z14 tile with three activities takes 136 µs, the narrowing done as a row pass and a column pass.
- A heat-points tile takes 3–6 ms to serve from z9, mostly the object read, and is up to 51 KB at z15 over a dense area. Up to z8, cells counted from mask rows took 11–25 ms a tile.

### Heat-points tiles encoded in Go — 2026-10-15, dev stack

Encoding heat-points tiles in Go in place of `ST_AsMVT` (`IMPLEMENTATION.md` §4.2.2), `browse -mode heatmap` over 8 seeded accounts, before and after. At 100 and 200 people a tile's database transactions fell from about 2.8 to 2.1, with p50 / p95 unchanged at 3 / 6–9 ms and database CPU within 3 points: the round trip removed was cheap. One of the two left is the session lookup.

### Fog as vector tiles — 2026-10-15, dev stack

Tracing Fog into vector tiles at request time (`IMPLEMENTATION.md` §4.2, Vector serving) against the PNG route, which only rewrites the stored bytes' header. `browse -mode fog` over 5 seeded accounts, vector or `-fog-png`, same build.

| Fog-only browse | p50 / p95, PNG → vector | API CPU mean (max), PNG → vector | Database CPU, PNG → vector |
| :-- | :-- | :-- | :-- |
| 100 people | 3 / 8 → 3 / 8 ms | 6 (13) → 8 (23)% | 33 → 25% |
| 200 people | 2 / 5 → 2 / 5 ms | 10 (16) → 14 (57)% | 36 → 34% |

- No request failed. The trace costs API CPU, 2–4 points on average with peaks while many first views trace together.
- A z14 vector tile is a median 234 bytes against the PNG's 1,743.
- Tracing takes about 210 µs plus 38 µs to decode the stored PNG, reusing the sample grid and simplifying each outline in one pass.