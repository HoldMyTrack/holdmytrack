-- IMPLEMENTATION.md §3.6: dirty_gen counts every time a tile is marked dirty, so a render
-- clears dirty only if nothing marked the tile again while it was being drawn.
ALTER TABLE fog_tiles ADD COLUMN dirty_gen BIGINT NOT NULL DEFAULT 0;
