-- HoldMyTrack keeps no health data, only the geographical data a user trusts it with
-- (VISION.md §1.1, ADR-0017). Heart rate was parsed out of uploads and stored here for the
-- pace/heart-rate profile; the profile is gone, and so is every stored reading. Nothing is
-- recomputed: no other column or metric was derived from it.
ALTER TABLE activity_streams DROP COLUMN heartrate;
