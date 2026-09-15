-- Password recovery. Same shape and reasoning as sessions (0006_sessions.sql): the row's own
-- gen_random_uuid() primary key *is* the reset token, no separate hashing layer — judged
-- sufficient for the same single-user threat model that decision was already made for.
CREATE TABLE password_resets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_password_resets_user ON password_resets (user_id);
