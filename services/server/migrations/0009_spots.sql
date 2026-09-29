-- Spots (IMPLEMENTATION.md §3.20, ADR-0021): outdoor places imported from OpenStreetMap. The
-- places are filled by the `import-spots` subcommand, not here.

CREATE TABLE spots (
    id           BIGSERIAL PRIMARY KEY,
    category     VARCHAR(16) NOT NULL
                 CHECK (category IN ('playground', 'dog_park', 'monument', 'viewpoint', 'history')),
    name         TEXT,                      -- OSM's `name`; NULL when it has none
    address      TEXT,                      -- built from OSM's addr:* tags; NULL when it has none
    -- OSM's own words about the place, each NULL when it has none: `description`, a memorial's
    -- `inscription`, its `memorial` type (statue, plaque, war_memorial…), `start_date` as
    -- written (a year, a date, "~1850"), and `wikipedia` as "lang:Article title".
    description  TEXT,
    inscription  TEXT,
    memorial     TEXT,
    start_date   TEXT,
    wikipedia    TEXT,
    -- The place's area: its OSM outline, or a 30 m circle around a place mapped as a point.
    geom         GEOMETRY(MultiPolygon, 4326) NOT NULL,
    osm_type     VARCHAR(8) NOT NULL CHECK (osm_type IN ('node', 'way', 'relation')),
    osm_id       BIGINT NOT NULL,
    UNIQUE (osm_type, osm_id)               -- the import's upsert key
);
CREATE INDEX idx_spots_geom ON spots USING GIST (geom);