-- Each Private location's secret (IMPLEMENTATION.md §4.1 step 3, §7): it seeds how far past
-- the circle's edge a track's end is cut, so the visible ends don't trace the circle. Random
-- per row, existing rows included; never sent to a client or written to a data export.
ALTER TABLE privacy_zones ADD COLUMN salt UUID NOT NULL DEFAULT gen_random_uuid();
