-- A per-user counter bumped whenever any of the account's map tiles (Fog, Heatmap, their
-- Country/Region tiers, tracks) may have changed — the key browsers and the app cache those
-- tiles under (IMPLEMENTATION.md §4.2's "Caching"). A counter, not a timestamp: it only ever
-- goes up, so a key once used is never reused for different content.
ALTER TABLE users ADD COLUMN map_version BIGINT NOT NULL DEFAULT 0;
