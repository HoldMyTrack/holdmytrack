-- VISION.md §5.3's personal bests: the fastest time each activity covered at least
-- one of five standard distances (internal/ingest.standardDistancesM). Same shape as
-- activity_tile_masks (0005) / activity_best_efforts (0010) — no user_id column, joined to
-- activities for user scoping.
CREATE TABLE activity_splits (
    activity_id  UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    distance_m   INT NOT NULL,  -- one of the five standard distances
    seconds      INT NOT NULL,  -- fastest time to cover at least distance_m
    PRIMARY KEY (activity_id, distance_m)
);

CREATE INDEX idx_activity_splits_distance ON activity_splits (distance_m);
