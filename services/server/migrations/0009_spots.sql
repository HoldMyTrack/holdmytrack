-- Spots (IMPLEMENTATION.md §3.20, ADR-0021): outdoor places imported from OpenStreetMap, and
-- the visits the user's own activities made to them. The places are filled by the
-- `import-spots` subcommand, not here.

CREATE TABLE spots (
    id        BIGSERIAL PRIMARY KEY,
    category  VARCHAR(16) NOT NULL
              CHECK (category IN ('playground', 'dog_park', 'monument', 'viewpoint', 'history')),
    name      TEXT,                         -- OSM's `name`; NULL when it has none
    address   TEXT,                         -- built from OSM's addr:* tags; NULL when it has none
    -- The place's area: its OSM outline, or a 50 m circle around a place mapped as a point.
    geom      GEOMETRY(MultiPolygon, 4326) NOT NULL,
    osm_type  VARCHAR(8) NOT NULL CHECK (osm_type IN ('node', 'way', 'relation')),
    osm_id    BIGINT NOT NULL,
    UNIQUE (osm_type, osm_id)               -- the import's upsert key
);
CREATE INDEX idx_spots_geom ON spots USING GIST (geom);

-- One row per (activity, spot) the activity spent at least five minutes inside. Replaced
-- whole for an activity every time it's processed; goes with the activity or the spot.
CREATE TABLE spot_visits (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    spot_id        BIGINT NOT NULL REFERENCES spots(id) ON DELETE CASCADE,
    activity_id    UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    visited_at     TIMESTAMPTZ NOT NULL,    -- the first point of the stay
    dwell_seconds  INT NOT NULL,
    PRIMARY KEY (activity_id, spot_id)
);
-- The spots tiles' "has this user visited this spot" lookup.
CREATE INDEX idx_spot_visits_user_spot ON spot_visits (user_id, spot_id);
CREATE INDEX idx_spot_visits_spot ON spot_visits (spot_id);