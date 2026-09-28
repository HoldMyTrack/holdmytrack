-- Stories (IMPLEMENTATION.md §3.19, ADR-0020): hand-picked, private sets of activities.
-- Deleting an activity cascades it out of every Story; a Story left empty stays. Deleting a
-- Story deletes no activity.

CREATE TABLE stories (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         VARCHAR(200) NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description  VARCHAR(2000),         -- NULL means never set
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()  -- a rename, a new description or a
                                                     -- membership change
);

CREATE INDEX idx_stories_user ON stories (user_id);

CREATE TABLE story_activities (
    story_id     UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    activity_id  UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    PRIMARY KEY (story_id, activity_id)
);

-- The primary key serves "a Story's activities"; this serves the activity-delete cascade and
-- "which Stories hold this activity".
CREATE INDEX idx_story_activities_activity ON story_activities (activity_id);
