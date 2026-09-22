-- Presets the Demo Customer account's (migrations/0017_demo_customer_user.sql) Settings-page
-- profile, so a "Try Demo" visitor sees a filled-out, plausible account rather than a blank
-- Name/Country and the timezone column's own UTC default. Country/Timezone match where the
-- seeded history (demo_presets.go's SeedDemoCustomer) actually is: its GPX files' own
-- coordinates are Lakewood/Cleveland, OH (confirmed directly, e.g. ~41.48, -81.83), which is
-- America/New_York, not UTC.
--
-- An UPDATE, not a second INSERT ... ON CONFLICT DO NOTHING like 0017's own seed — the row
-- already exists by the time this runs, so a DO NOTHING insert would never apply these.
--
-- privacy_trim_m = 15 is deliberately not a round meters figure: it's the closest achievable
-- value to "a real US-based visitor typed 50 into the Settings field" (50 ft * 0.3048 = 15.24,
-- rounded to 15). It does NOT redisplay as exactly "50 ft" — 15m round-trips back through
-- apps/web/src/ui/format.ts's metersToFeet/Math.round as 49 ft, one off. That is a general
-- limitation of storing this column as whole meters while displaying it in feet (there is no
-- integer-meters value at all that redisplays as exactly 50 ft), not something specific to
-- this seed value; confirmed no nearby integer does better. Left as 15 rather than hunting for
-- a "cleaner" number, since every value has the same one-off-in-either-direction problem.
UPDATE users
SET display_name = 'Demo User',
    country = 'US',
    timezone = 'America/New_York',
    privacy_trim_m = 15
WHERE id = '22222222-2222-2222-2222-222222222222';
