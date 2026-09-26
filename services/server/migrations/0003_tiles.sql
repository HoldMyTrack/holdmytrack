-- Rendered coverage: fog_tiles (IMPLEMENTATION.md §3.6), activity_tile_masks (§3.11) and
-- user_tiles (§3.5).

-- §3.6 fog_tiles -- the cached "whole history, nothing hidden" composite per tile, built by
-- compositing activity_tile_masks.
CREATE TABLE fog_tiles (
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom                SMALLINT NOT NULL,
    tile_x              INT NOT NULL,
    tile_y              INT NOT NULL,
    object_key          TEXT,
    heatmap_object_key  TEXT,  -- §4.2.2's additive-intensity raster -- shares this row rather
                               -- than a separate heatmap_tiles table, since both rasters go
                               -- dirty from exactly the same ingest/reprivacy events
    dirty               BOOLEAN NOT NULL DEFAULT TRUE,
    rendered_at         TIMESTAMPTZ,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);

CREATE INDEX idx_fog_dirty ON fog_tiles (user_id) WHERE dirty;

-- §3.11 activity_tile_masks -- one crisp (unblurred) stroke per activity per z14 tile it
-- touches, rendered once at ingest from points already in memory. This is what lets fog and
-- heatmap answer a date-range or hidden-activity filter by compositing fewer masks rather than
-- re-parsing raw files per request (§4.2.3, internal/fog/render.go's RenderActivityMasks).
CREATE TABLE activity_tile_masks (
    activity_id      UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    zoom             SMALLINT NOT NULL DEFAULT 14, -- always 14 today; kept for schema symmetry with fog_tiles
    tile_x           INT NOT NULL,
    tile_y           INT NOT NULL,
    mask_object_key  TEXT NOT NULL,
    rendered_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (activity_id, zoom, tile_x, tile_y)
);

-- Both read paths ask "which activities touch tile (zoom, x, y)", never the reverse.
CREATE INDEX idx_activity_tile_masks_tile ON activity_tile_masks (zoom, tile_x, tile_y);

-- §3.5 user_tiles -- explorer-tile scoring (§4.4). Nothing writes it yet.
CREATE TABLE user_tiles (
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom              SMALLINT NOT NULL,   -- 14 (~2.4 km) and 17 (~306 m) at the equator
    tile_x            INT NOT NULL,
    tile_y            INT NOT NULL,
    first_visited_at  TIMESTAMPTZ NOT NULL,
    visit_count       INT NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);
