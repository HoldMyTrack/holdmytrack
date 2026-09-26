-- Replaces the Demo Customer's Private locations. The synthetic "Home" 0027_private_locations.sql
-- added sat over the start cluster of the generated Cleveland history demo_data/ no longer ships;
-- these three are where the real history (export-demo-activities) actually starts and ends, so
-- ingest clips them the way it would for any account (FR-8.1).
--
-- radius_m = 305 is the closest whole-meters value to 1000 ft (304.8 m). Like 0023's
-- privacy_trim_m it can't redisplay exactly: 305 m shows as 1001 ft, and 304 m as 997 ft.
DELETE FROM privacy_zones WHERE user_id = '22222222-2222-2222-2222-222222222222';

INSERT INTO privacy_zones (user_id, name, center, radius_m)
SELECT u.id, z.name, ST_SetSRID(ST_MakePoint(z.lon, z.lat), 4326)::geography, 305
FROM users u
CROSS JOIN (VALUES
    ('Home',            41.388222, -81.739806),  -- 41°23'17.6"N 81°44'23.3"W
    ('Eva''s school',   41.397861, -81.734556),  -- 41°23'52.3"N 81°44'04.4"W
    ('Sonya''s school', 41.482333, -81.736833)   -- 41°28'56.4"N 81°44'12.6"W
) AS z(name, lat, lon)
WHERE u.id = '22222222-2222-2222-2222-222222222222';
