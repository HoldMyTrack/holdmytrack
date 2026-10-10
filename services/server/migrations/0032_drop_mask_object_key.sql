-- §3.11 activity_tile_masks: drop mask_object_key, which named each mask's object in R2 before
-- 0030 moved the PNGs into activity_tile_mask_data (ADR-0040). Safe beside the previous
-- release: it never reads or writes the column, and no row has a key.
ALTER TABLE activity_tile_masks DROP COLUMN mask_object_key;
