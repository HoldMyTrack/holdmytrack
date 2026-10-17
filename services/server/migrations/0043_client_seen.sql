-- users: the day each client was last used (IMPLEMENTATION.md §4.32), so the daily snapshot can
-- count who uses the website, the Android app or both, and which Android builds are still in use
-- before MIN_ANDROID_BUILD retires one. Written with last_seen_at, at most once a UTC day per
-- client, and when the Android build changes.
ALTER TABLE users
    ADD COLUMN web_seen_at     TIMESTAMPTZ,
    ADD COLUMN android_seen_at TIMESTAMPTZ,
    ADD COLUMN android_build   INTEGER;
