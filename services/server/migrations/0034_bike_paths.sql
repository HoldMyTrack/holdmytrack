-- Bike paths (IMPLEMENTATION.md §3.27, §4.24): OpenStreetMap's cycleways and the paths it marks
-- as designated for bikes, the same for every account. Filled by the `import-bike-paths`
-- subcommand, not here.

CREATE TABLE bike_paths (
    id               BIGSERIAL PRIMARY KEY,
    -- `cycleway`: highway=cycleway, built for bikes. `shared`: a path, footway or bridleway
    -- tagged bicycle=designated, a multi-use trail bikes share with people on foot.
    kind             VARCHAR(8) NOT NULL CHECK (kind IN ('cycleway', 'shared')),
    name             TEXT,                  -- OSM's `name`; NULL when it has none
    -- Web Mercator, the tiles' own projection: a tile clips, simplifies and measures it as
    -- stored, with nothing to reproject per request.
    geom             GEOMETRY(MultiLineString, 3857) NOT NULL,
    osm_id           BIGINT NOT NULL UNIQUE, -- the OSM way; the import's upsert key
    -- When the import run that last had the way started: a planet run deletes the rows it
    -- didn't see.
    last_seen_import TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_bike_paths_geom ON bike_paths USING GIST (geom);
