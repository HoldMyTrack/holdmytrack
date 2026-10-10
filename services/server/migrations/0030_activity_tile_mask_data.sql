-- §3.11 activity_tile_mask_data: each activity mask's PNG, in Postgres instead of object
-- storage (ADR-0040). Its own table, so activity_tile_masks stays the narrow index a render
-- scans for "which activities touch this tile", and a backup can leave the bytes out
-- (scripts/backup.sh): they're redrawn from raw uploads by `rerender-coverage --masks`.
-- Safe beside the previous release: it never reads this table, and still writes a key.
CREATE TABLE activity_tile_mask_data (
    activity_id  UUID NOT NULL,
    zoom         SMALLINT NOT NULL,
    tile_x       INT NOT NULL,
    tile_y       INT NOT NULL,
    png          BYTEA NOT NULL,
    PRIMARY KEY (activity_id, zoom, tile_x, tile_y),
    -- The bytes go with their row, and so with their activity.
    FOREIGN KEY (activity_id, zoom, tile_x, tile_y)
        REFERENCES activity_tile_masks (activity_id, zoom, tile_x, tile_y) ON DELETE CASCADE
);

-- A mask written since has its bytes above and no object. Rows from before keep their key
-- until `rerender-coverage --masks` redraws them (docs/DEPLOY.md §6).
ALTER TABLE activity_tile_masks ALTER COLUMN mask_object_key DROP NOT NULL;
