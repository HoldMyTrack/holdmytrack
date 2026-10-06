-- IMPLEMENTATION.md §4.7.8: an activity split into pieces. Every piece is its own row over the
-- same raw payload, covering the recorded points from split_from to split_to (unix ms,
-- inclusive; NULL is that end of the recording). split_group is the id of the row first split,
-- shared by every piece of it, and is what Merge checks pieces against. No foreign key: the
-- group outlives that row being deleted.
ALTER TABLE activities
    ADD COLUMN split_group UUID,
    ADD COLUMN split_from  BIGINT,
    ADD COLUMN split_to    BIGINT;

CREATE INDEX idx_activities_split_group ON activities (split_group) WHERE split_group IS NOT NULL;
