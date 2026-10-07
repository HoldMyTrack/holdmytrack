-- IMPLEMENTATION.md §4.1: a track across the antimeridian is now stored continuing past ±180
-- instead of jumping back across the world, and its masks and tiles wrap round the world's
-- edge. One stored before has a trajectory spanning every longitude, masks along a whole
-- parallel, and the countries that line crossed. Each account with one gets a `reprivacy` job
-- for them (the same as ingest.EnqueueReprivacy queues): reprocessing rebuilds the trajectory,
-- masks, tiles and matches from the raw payload. Pending meanwhile, as an edit would be.
CREATE TEMP TABLE crossing ON COMMIT DROP AS
SELECT user_id, array_agg(id::text ORDER BY id) AS ids
FROM activities
WHERE trajectory IS NOT NULL AND raw_payload_key IS NOT NULL
  AND ST_XMax(trajectory) - ST_XMin(trajectory) > 180
  AND ST_XMax(ST_ShiftLongitude(trajectory)) - ST_XMin(ST_ShiftLongitude(trajectory)) < ST_XMax(trajectory) - ST_XMin(trajectory)
GROUP BY user_id;

UPDATE activities a SET edit_pending = true
FROM crossing c WHERE a.user_id = c.user_id AND a.id::text = ANY(c.ids);

INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty)
SELECT DISTINCT c.user_id, m.zoom, m.tile_x, m.tile_y, true
FROM crossing c JOIN activity_tile_masks m ON m.activity_id::text = ANY(c.ids)
WHERE m.zoom = 14
ON CONFLICT (user_id, zoom, tile_x, tile_y) DO UPDATE SET dirty = true, dirty_gen = fog_tiles.dirty_gen + 1;

UPDATE users u SET map_version = map_version + 1 FROM crossing c WHERE u.id = c.user_id;

INSERT INTO jobs (kind, user_id, payload)
SELECT 'reprivacy', user_id, json_build_object('user_id', user_id, 'activity_ids', ids)
FROM crossing;
