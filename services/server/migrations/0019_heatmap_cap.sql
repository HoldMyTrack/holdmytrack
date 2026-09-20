-- Replaces internal/fog/raster.go's fixed heatmapCap = 8.0 (the accumulated-intensity value
-- that maps to full saturation) with a per-account value: internal/fog.RecomputeHeatmapCap
-- derives it from the 95th percentile of how many in-window activities touch each z14 tile,
-- and internal/worker/heatmap_cap.go's daily sweep keeps it current, updating this column and
-- marking the account's fog_tiles dirty only when the value moves enough to matter.
--
-- Default 8.0 reproduces today's exact rendered output for every existing account, byte for
-- byte, until the sweep first recomputes it — unlike 0018's in_heatmap_window, there is no
-- real historical value to backfill here, so no UPDATE follows this ALTER TABLE.
ALTER TABLE users ADD COLUMN heatmap_cap DOUBLE PRECISION NOT NULL DEFAULT 8.0;
