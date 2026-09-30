-- Spot captures (IMPLEMENTATION.md §4.25, ADR-0023): a spot an account captured by staying 30 s
-- inside it with the Android app's capture mode on. One per account and place; gone with either.

CREATE TABLE spot_captures (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    spot_id      BIGINT NOT NULL REFERENCES spots(id) ON DELETE CASCADE,
    captured_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, spot_id)
);
