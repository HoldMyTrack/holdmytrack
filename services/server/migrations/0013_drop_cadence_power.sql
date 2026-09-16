-- Unused end-to-end: cadence and power_w were parsed and stored (0001_init.sql) but never
-- read back by any query, API response, or client. FitMap's scope is outdoor GPS tracking
-- (VISION.md §1.1), not a sports-computer sensor product -- heartrate and elevation_m stay,
-- since they feed the per-activity pace/HR profile (GET /v1/activities/track-metrics/{id}).
ALTER TABLE activity_streams DROP COLUMN cadence;
ALTER TABLE activity_streams DROP COLUMN power_w;
