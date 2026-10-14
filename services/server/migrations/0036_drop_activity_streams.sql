-- Per-point streams (IMPLEMENTATION.md §5.7): nothing read them, and ingest can derive them again
-- from an activity's raw payload if a feature ever needs them.
DROP TABLE activity_streams;
