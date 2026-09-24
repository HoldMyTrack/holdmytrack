-- Private locations replace the fixed endpoint Privacy Trim (docs/adr/0010-private-locations-replace-endpoint-trim.md).
-- Trimming every track's ends hid nothing on a multi-day trail — each day starts where the last
-- one stopped — while cutting a gap into the fog at every day boundary. The places worth hiding
-- are the ones a user names, so the setting goes and privacy_zones (§3.7) finally gets used.
--
-- No backfill: activities already ingested keep the trim they were processed with. One gets its
-- ends back only when something reprocesses it from its raw payload — an Edit track, or a
-- Private location change whose affected set includes it.
ALTER TABLE users DROP COLUMN privacy_trim_cm;

ALTER TABLE privacy_zones
    ADD COLUMN name       TEXT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ALTER COLUMN radius_m SET DEFAULT 200;

-- The Demo Customer account (0017) records nearly every trip from one home. Its location is
-- deliberately not centered on that home: a circle's middle is the first place anyone looks.
INSERT INTO privacy_zones (user_id, name, center, radius_m)
SELECT id, 'Home', ST_SetSRID(ST_MakePoint(-81.7395, 41.4020), 4326)::geography, 250
FROM users WHERE id = '22222222-2222-2222-2222-222222222222';
