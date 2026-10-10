# ADR-0040: Activity masks' PNGs live in a Postgres table of their own, left out of backups

## Status

Accepted. Built: `migrations/0030_activity_tile_mask_data.sql`, `internal/fog/render.go` (`RenderActivityMasks`, `renderAndStoreTile`, `RemoveActivityMasks`), `scripts/backup.sh` (`IMPLEMENTATION.md` §3.11, §4.2.3).

## Context

Each activity has a crisp mask per z14 tile it touches (`activity_tile_masks`, `IMPLEMENTATION.md` §4.2.3), which every render of that tile composites. The masks were objects in R2, one per row. Since ADR-0037 they are 64 px: production had 5,777 of them for 744 activities, about 8 an activity and about 275 bytes each.

The 2026-10-09 production load test (`PERFORMANCE.md`) found a render to be mostly waiting. `renderAndStoreTile` made one R2 GET for each mask on every dirty z14 tile, so a big import's renders held the worker for 10–15 minutes each at about 81% of its 200% CPU. With objects this small, each fetch is almost all round trip.

## Decision

**A mask's PNG is stored in `activity_tile_mask_data`, beside its `activity_tile_masks` row, and a render reads it in the same query that finds the tile's masks.** `RenderActivityMasks` encodes each mask in memory and writes both rows in one transaction. A z14 tile then costs one SQL read for all its masks instead of a round trip per mask. The bytes stay well under Postgres's roughly 2 KB inline-storage threshold, about 600 bytes a mask at 128 px (ADR-0041), so they aren't moved out to TOAST.

**The bytes get a table of their own, not a column on `activity_tile_masks`.** `activity_tile_masks` stays the narrow table a render scans for "which activities touch this tile" and the Heatmap cap's count groups (`fog.RecomputeHeatmapCap`). The bytes reference it with `ON DELETE CASCADE`, so they go with their row and, through it, with their activity.

**Backups leave the table's data out** (`pg_dump --exclude-table-data`). The masks are derived data, redrawn from raw uploads by `rerender-coverage --masks`, as the tile objects already were (ADR-0029). So the dump doesn't grow with them, and the restore steps already end with that rerun.

**The move is gradual.** `mask_object_key` became nullable. A row written before it keeps its key, and a render fetches that object from R2 until `rerender-coverage --masks` redraws the row into the database. A row with neither bytes nor a key (after a restore, before that rerun) is left out of the tile, and each pass logs how many it skipped. A later migration drops the column, the R2 fallback and the `activity-masks/` prefix removals once no row has a key.

## Alternatives considered

- **Keep R2 and add an LRU cache of masks in the worker.** Rejected: a cold render, the first after an import or a deploy, is the one that matters, and it would still make a round trip per mask.
- **Plain files on a volume.** Rejected. A 275-byte file takes a 4 KB block. A write couldn't share a transaction with `activities`. `api`, `worker` and one-off `run --rm` containers would need one shared volume. It would also be a second store outside the backups.
- **SQLite on the worker.** Kept as the fallback if mask reads turn out to compete with map queries in Postgres. It would mean a second database to run, and every writer of masks (ingest, edit, reprivacy, the `rerender-coverage` command) would have to reach the worker's disk.
- **A `bytea` column on `activity_tile_masks`.** Rejected, for the narrow scan and the backup exclusion above.

## Consequences

- A render's z14 tile is one query, no object-storage reads. Its two PUTs and the pyramid's reads remain.
- The database grows by about 5 KB per activity at 128 px (2 KB at 64 px). At a million activities that is about 5 GB, in a table the backups leave out.
- Mask reads now share Postgres's CPU and cache with the map's queries, which were already the first thing to run out at 200 users. `PERFORMANCE.md` records the measurement of a render under load.
- After a restore, Fog and Heatmap are missing every activity until `rerender-coverage --masks` finishes; the restore steps already end with it (`DEPLOY.md` §11).
