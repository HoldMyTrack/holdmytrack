-- IMPLEMENTATION.md §4.23, ADR-0036: sending a copy of a Story to another account.
--
-- story_sends is the recipient's inbox: a copy offered and not yet accepted or declined. Repeat
-- sends of one Story to one person are one row; deleting the Story withdraws it.
CREATE TABLE story_sends (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id      UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    recipient_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (story_id, recipient_id)
);
CREATE INDEX idx_story_sends_recipient ON story_sends (recipient_id);

-- story_copies is where a Story's copy went for each recipient, so the next accepted send of
-- the same Story adds to it. The row goes with either Story.
CREATE TABLE story_copies (
    source_story_id  UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    recipient_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    copy_story_id    UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    PRIMARY KEY (source_story_id, recipient_id)
);
CREATE INDEX idx_story_copies_copy ON story_copies (copy_story_id);

-- An activity made by a copy remembers the original it descends from, however many copies
-- removed: no foreign key, since the original may be deleted while its copies stay.
ALTER TABLE activities ADD COLUMN origin_id UUID;

-- Every original an account has received a copy of, kept after the copy is deleted: an
-- account receives each original once, ever.
CREATE TABLE received_origins (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    origin_id  UUID NOT NULL,
    PRIMARY KEY (user_id, origin_id)
);
