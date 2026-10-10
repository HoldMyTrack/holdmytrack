-- §3.3 activities: recorded_started_at and recorded_ended_at, the stretch of time the upload (or
-- its split piece) covers as recorded — before Private locations clip its ends and before any
-- track edit. The overlap hint compares these (§4.6): started_at and duration_seconds describe
-- the clipped track, so a walk from home lost the minutes inside the Private location and fell
-- under the 80% another source's full copy of it needs. Ingest and every reprocess set them;
-- rows stored before are filled by `backfill-recorded-spans`, and until then the hint falls back
-- to started_at and duration_seconds. Safe beside the previous release, which never reads them.
ALTER TABLE activities
    ADD COLUMN recorded_started_at timestamptz,
    ADD COLUMN recorded_ended_at timestamptz;

-- The overlap lookup's range scan, on the start it compares.
CREATE INDEX idx_activities_user_recorded_start
    ON activities (user_id, (COALESCE(recorded_started_at, started_at)));
