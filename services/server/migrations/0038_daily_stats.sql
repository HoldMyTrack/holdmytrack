-- Service statistics (IMPLEMENTATION.md §4.32): one row per UTC day and stat, written once by
-- the worker's daily snapshot and never updated. Aggregates across accounts only, so no account
-- id and no foreign key; a new stat is a new key, not a new column.
CREATE TABLE daily_stats (
    day       DATE             NOT NULL,   -- the UTC day the values describe
    key       TEXT             NOT NULL,   -- e.g. 'active_30d', 'activities_by_source.healthconnect'
    value     DOUBLE PRECISION NOT NULL,   -- a count, a byte sum or a percentile
    taken_at  TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    PRIMARY KEY (day, key)
);
