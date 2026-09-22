-- Fixes the Privacy Trim round-trip bug (docs/KNOWN_ISSUES.md): an imperial account's feet
-- input went through Math.round(feetToMeters(...)) on save and Math.round(metersToFeet(...))
-- on reload, and two roundings to whole meters compound — "50" ft saved and reloaded as "49"
-- ft, with no integer-meters value at all redisplaying as exactly 50 ft. Storing at
-- centimeter precision instead removes the error: 1 ft = 30.48 cm exactly, so every whole-foot
-- input round-trips through a single cm-precision rounding with no accumulated drift
-- (confirmed for every 1-2000 ft value). Metric input is already exact either way (whole
-- meters is a round number of centimeters too).
ALTER TABLE users RENAME COLUMN privacy_trim_m TO privacy_trim_cm;
UPDATE users SET privacy_trim_cm = privacy_trim_cm * 100;
ALTER TABLE users ALTER COLUMN privacy_trim_cm SET DEFAULT 10000;

-- The Demo Customer account's own preset (migrations/0023_demo_customer_profile.sql) landed on
-- 15 m specifically because a real US visitor's "50" ft had no exact whole-meters
-- representation to store. Centimeters has one: 50 ft = 1524 cm exactly.
UPDATE users SET privacy_trim_cm = 1524 WHERE id = '22222222-2222-2222-2222-222222222222';
