-- Activity photos (IMPLEMENTATION.md §4.27, ADR-0024): the user's own pictures on an activity,
-- stored resized under photos/{user_id}/ in object storage. A photo's place on the map is not
-- stored — route_at is a moment on the activity's trajectory (its M axis), and the position is
-- worked out from the track on every read, so Edit track and Private locations apply to it.
-- Every photo has one: the server places it by capture time or position, or the user does.

CREATE TABLE activity_photos (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    activity_id         UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    taken_at            TIMESTAMPTZ,            -- the file's EXIF capture time; NULL when it had none
    route_at            TIMESTAMPTZ NOT NULL,   -- where on the track
    content_type        VARCHAR(32) NOT NULL,   -- image/jpeg | image/webp, sniffed
    thumb_content_type  VARCHAR(32) NOT NULL,
    width               INT NOT NULL,
    height              INT NOT NULL,
    bytes               INT NOT NULL,           -- the resized copy and its thumbnail together
    caption             TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_activity_photos_activity ON activity_photos (activity_id);
CREATE INDEX idx_activity_photos_user ON activity_photos (user_id);
