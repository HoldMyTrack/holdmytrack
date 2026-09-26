-- The shared Demo Customer account every "Try Demo" session points at (IMPLEMENTATION.md
-- §4.10; the fixed id is internal/httpapi.DemoCustomerUserID). Its activities are seeded by the
-- `seed-demo-customer` subcommand, not here.
--
-- demo_expires_at is far in the future rather than NULL: non-NULL keeps the account read-only
-- (requireNotDemo) and exempt from email verification (requireVerified), and
-- internal/worker/demo_purge.go's `demo_expires_at < NOW()` sweep never matches it.
-- Country/timezone match where its history actually is (Lakewood/Cleveland, OH).
INSERT INTO users (id, email, demo_expires_at, display_name, country, timezone)
VALUES ('22222222-2222-2222-2222-222222222222', 'demo-customer@holdmytrack.invalid',
        '9999-12-31 00:00:00+00', 'Demo User', 'US', 'America/New_York');

-- Where its history starts and ends, so ingest clips them the way it would for any account
-- (FR-8.1). radius_m = 305 is the closest whole-meters value to 1000 ft (304.8 m).
INSERT INTO privacy_zones (user_id, name, center, radius_m)
SELECT '22222222-2222-2222-2222-222222222222', z.name, ST_SetSRID(ST_MakePoint(z.lon, z.lat), 4326)::geography, 305
FROM (VALUES
    ('Home',            41.388222, -81.739806),  -- 41°23'17.6"N 81°44'23.3"W
    ('Eva''s school',   41.397861, -81.734556),  -- 41°23'52.3"N 81°44'04.4"W
    ('Sonya''s school', 41.482333, -81.736833)   -- 41°28'56.4"N 81°44'12.6"W
) AS z(name, lat, lon);
