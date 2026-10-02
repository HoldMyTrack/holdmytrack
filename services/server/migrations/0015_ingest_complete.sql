-- IMPLEMENTATION.md §4.1 step 6: an ingest commits the activity row first and derives the
-- rest (streams, masks, dedupe, regions, the render) after it. ingest_complete is false until
-- that's all done, so a job cut off in between, or a re-upload after it failed, resumes the
-- activity instead of finding it "already processed" and leaving it half-built for good.
ALTER TABLE activities ADD COLUMN ingest_complete BOOLEAN NOT NULL DEFAULT true;
