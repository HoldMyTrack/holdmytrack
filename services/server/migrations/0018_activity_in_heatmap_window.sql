-- Heatmap's rolling window (fog.HeatmapWindowDays) used to be recomputed against "now" on
-- every render, but nothing ever re-evaluated it purely on the passage of time -- an activity
-- had to be touched by an ingest or delete event to get re-checked at all. This column makes
-- window membership a stored fact instead of a live computation: internal/worker's daily
-- heatmap_aging.go sweep is the only thing that ever flips it (true -> false, one-way, since an
-- activity's started_at only ever gets further in the past), and internal/fog.renderAndStoreTile
-- just reads it -- see that file's own comment.
--
-- Defaults true: a freshly ingested activity is, by definition, within the window as of now.
-- The backfill below is the one-time exception -- existing rows need their true historical
-- state, computed against the same window this column is meant to track from here on.
ALTER TABLE activities ADD COLUMN in_heatmap_window BOOLEAN NOT NULL DEFAULT true;

UPDATE activities SET in_heatmap_window = (started_at >= NOW() - INTERVAL '365 days');

-- The daily sweep's own query (heatmap_aging.go) is "still-in-window rows old enough to have
-- aged out" -- a partial index on exactly that predicate keeps it index-only rather than a
-- growing sequential scan over the whole table as history accumulates.
CREATE INDEX idx_activities_in_heatmap_window ON activities (started_at) WHERE in_heatmap_window;
