-- Cross-source deduplication (IMPLEMENTATION.md §4.6). The same ride can arrive three times --
-- pushed by a provider, synced from the phone's health store, and again in a bulk export --
-- with three genuinely different external_ids, which is exactly why 0001_init.sql's
-- idx_activities_dedupe cannot catch it.
--
-- Superseded rather than deleted, per §4.6: a user has to be able to see why an activity
-- disappeared, and a row that quietly vanished answers nothing.
--
-- ON DELETE SET NULL is deliberate and is the useful behaviour, not a default: deleting the
-- copy that won puts the copy it displaced back in the map and the totals, rather than
-- orphaning it into permanent invisibility.
ALTER TABLE activities
    ADD COLUMN superseded_by UUID REFERENCES activities(id) ON DELETE SET NULL;

-- Every user-facing read is now "this user's live activities, newest first", so the index
-- those reads actually want is the partial one. idx_activities_user_time stays for the
-- queries that legitimately span both (the delete path, and the duplicates listing).
CREATE INDEX idx_activities_live ON activities (user_id, started_at DESC)
    WHERE superseded_by IS NULL;

-- The collision lookup: §4.6's tolerance is "start time within a minute, distance within ~1%",
-- and it is applied as a range around the incoming activity rather than as equality on a
-- pre-rounded bucket. Rounding first makes matching a lottery at the bucket edges -- measured
-- on a real pair three seconds apart that landed either side of a minute boundary and so was
-- never compared. This index is what makes the range scan cheap.
CREATE INDEX idx_activities_dedupe_window ON activities (user_id, activity_type, started_at);

-- dedupe_key (0001_init.sql) held a pre-rounded fingerprint for exactly the equality match
-- that is no longer done. It was never populated by any released code, so there is nothing to
-- migrate -- it goes rather than staying behind as a column §3.3 describes and nothing writes.
DROP INDEX IF EXISTS idx_activities_crosssource;
ALTER TABLE activities DROP COLUMN dedupe_key;
