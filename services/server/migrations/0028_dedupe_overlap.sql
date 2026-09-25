-- Cross-source dedup matches on time overlap now, not on type + start + distance (§4.6). The
-- candidate lookup is a range scan on (user_id, started_at), which idx_activities_user_time
-- already serves; the type-leading index existed only for the old match.
--
-- No backfill: activities already ingested keep whatever the old rule decided about them.
DROP INDEX IF EXISTS idx_activities_dedupe_window;
