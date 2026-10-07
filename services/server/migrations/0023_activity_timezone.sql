-- IMPLEMENTATION.md §3.3, §4.30, ADR-0035: each activity's own timezone, the one it was recorded
-- in, so its times show and its days group as the wall clock read where it happened.
--
-- The zone polygons come from timezone-boundary-builder, loaded by `seed-timezones` and cut into
-- small pieces (ST_Subdivide) like admin_country_parts, so finding a point's zone stays an index
-- lookup. Until the seed has run there are none, and every activity keeps its account's zone.
CREATE TABLE tz_parts (
    tzid TEXT NOT NULL,                       -- IANA name, e.g. 'Asia/Tokyo', 'Etc/GMT+5' at sea
    geom GEOMETRY(Polygon, 4326) NOT NULL
);
CREATE INDEX idx_tz_parts_geom ON tz_parts USING GIST (geom);

-- Which release the polygons came from, so re-running the seed with the same file skips the
-- reload and the re-match of every activity.
CREATE TABLE tz_boundaries_source (
    only_row  BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (only_row),
    name      TEXT NOT NULL,
    loaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The zone's IANA name, never an offset: the tz database keeps each zone's history, so
-- started_at AT TIME ZONE timezone is the offset in force on the activity's own date.
ALTER TABLE activities ADD COLUMN timezone TEXT;
UPDATE activities a SET timezone = u.timezone FROM users u WHERE u.id = a.user_id;
ALTER TABLE activities ALTER COLUMN timezone SET NOT NULL;
