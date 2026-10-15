-- users: the zoomed-out Heatmap's scale (IMPLEMENTATION.md §4.2.2) — for each heat-points zoom up
-- to fog.HeatActivityMaxZoom, at index z + 1, the most in-window activities any one of its cells
-- holds. Stored by every coverage render; NULL until the account's next, when heatmap_cap stands in.
ALTER TABLE users ADD COLUMN heat_point_caps INT[];
