-- VISION.md §5.3's best-effort curves: the maximal average pace/heart-rate sustained
-- over each standard duration (internal/ingest.bestEffortWindowsS), one row per (activity,
-- metric, window) actually achieved. Same shape as activity_tile_masks (0005) — no user_id
-- column, joined to activities for user scoping, the established pattern for per-activity
-- derived data in this schema.
CREATE TABLE activity_best_efforts (
    activity_id  UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    metric       VARCHAR(16) NOT NULL,  -- 'pace' | 'heartrate'
    window_s     INT NOT NULL,
    value        REAL NOT NULL,         -- m/s for pace, avg bpm for heartrate — both
                                         -- "higher is better," so one MAX aggregate covers
                                         -- both without a sign flip
    PRIMARY KEY (activity_id, metric, window_s)
);

CREATE INDEX idx_best_efforts_metric_window ON activity_best_efforts (metric, window_s);
