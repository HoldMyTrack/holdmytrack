-- Per-activity z14 fog/heatmap contribution masks — one crisp (unblurred) stroke per
-- activity per tile it touches, rather than one aggregate-per-user raster. This is what lets
-- fog/heatmap answer a date-range or hidden-activity filter without re-parsing raw GPS files
-- per request: filtering is "composite fewer of these small pre-rendered masks," not
-- "re-render from scratch." See internal/fog/render.go's RenderActivityMasks doc comment.
--
-- Rendered once at ingest, from points already in memory — no raw-payload re-fetch needed
-- for the activity that triggered it. fog_tiles (0001, 0004) is unchanged in shape and
-- meaning: it stays the cached "everyone, whole history, nothing hidden" composite, now
-- computed by compositing these masks instead of re-parsing raw files.
CREATE TABLE activity_tile_masks (
    activity_id      UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    zoom             SMALLINT NOT NULL DEFAULT 14, -- always 14 today; kept for schema symmetry with fog_tiles
    tile_x           INT NOT NULL,
    tile_y           INT NOT NULL,
    mask_object_key  TEXT NOT NULL,
    rendered_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (activity_id, zoom, tile_x, tile_y)
);

-- Both render_fog's per-tile lookup and the filtered on-the-fly path query "which activities
-- touch tile (zoom, x, y)", never the reverse — activity_id is already the PK's leading
-- column for that direction, unused by either read path.
CREATE INDEX idx_activity_tile_masks_tile ON activity_tile_masks (zoom, tile_x, tile_y);
