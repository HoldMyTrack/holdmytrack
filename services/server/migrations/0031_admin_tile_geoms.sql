-- §3.26 admin_tile_geoms and admin_tiles_built: the Country and Region tiles' outlines, each
-- already transformed and clipped to a tile, so serving one is a lookup rather than drawing
-- every outline in it again on every request (IMPLEMENTATION.md §4.2.4). The same for every
-- account; only which outlines a tile shows is the account's own. Built on a tile's first
-- request and emptied by seed-admin-boundaries. Safe beside the previous release, which never
-- reads them.
CREATE TABLE admin_tile_geoms (
    layer     TEXT NOT NULL,      -- 'countries' or 'regions', the tile's MVT layer
    zoom      SMALLINT NOT NULL,
    tile_x    INT NOT NULL,
    tile_y    INT NOT NULL,
    admin_id  INT NOT NULL,       -- admin_countries.id or admin_regions.id, by layer
    geom      GEOMETRY NOT NULL,  -- ST_AsMVTGeom's output, in the tile's 4096-unit grid
    PRIMARY KEY (layer, zoom, tile_x, tile_y, admin_id)
);

-- A tile built, including one with no outline in it.
CREATE TABLE admin_tiles_built (
    layer   TEXT NOT NULL,
    zoom    SMALLINT NOT NULL,
    tile_x  INT NOT NULL,
    tile_y  INT NOT NULL,
    PRIMARY KEY (layer, zoom, tile_x, tile_y)
);
