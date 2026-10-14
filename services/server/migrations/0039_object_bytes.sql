-- Object sizes for the service statistics' storage values (IMPLEMENTATION.md §4.32): each
-- stored object's size in bytes beside the key that names it, set when it's written. NULL is
-- not measured yet: a row from before this column, until `backfill-object-bytes` lists the
-- bucket and fills it. Activity masks need none: their bytes are in activity_tile_mask_data.
ALTER TABLE activities ADD COLUMN raw_bytes BIGINT;  -- the object at raw_payload_key

ALTER TABLE fog_tiles
    ADD COLUMN bytes          INT,  -- the object at object_key
    ADD COLUMN heatmap_bytes  INT;  -- the object at heatmap_object_key
