-- IMPLEMENTATION.md §4.27, ADR-0036: a photo's image files are named by image_key rather than
-- by the row's own owner and id, so a copy of a Story (§4.23) can give the recipient photo rows
-- of their own over the same files. The thumbnail is image_key || '-thumb'. A file is removed
-- only when no row refers to its key any more.
ALTER TABLE activity_photos ADD COLUMN image_key TEXT;
UPDATE activity_photos SET image_key = 'photos/' || user_id || '/' || id;
ALTER TABLE activity_photos ALTER COLUMN image_key SET NOT NULL;

CREATE INDEX idx_activity_photos_image_key ON activity_photos (image_key);
