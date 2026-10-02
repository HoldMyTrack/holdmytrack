-- Spots places refresh (IMPLEMENTATION.md §4.25, ADR-0027): a place a planet import no longer
-- sees is retired, never deleted, so the captures that refer to it stay.
--   last_seen_import: when the import run that last had the place started; NULL before any run
--                     stamped it, which a planet run treats as unseen.
--   retired_at:       when a planet run found it gone from OSM; NULL for a live place.
ALTER TABLE spots
    ADD COLUMN last_seen_import TIMESTAMPTZ,
    ADD COLUMN retired_at       TIMESTAMPTZ;
