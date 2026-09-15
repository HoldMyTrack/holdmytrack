-- Simple email+password auth, server-side sessions — §5.2's real-accounts work, resolved
-- toward the smallest thing that removes PlaceholderUserID rather than a third-party
-- identity provider: no vendor to depend on, matching the same bias Path 3 uploads already
-- made (services/server/internal/httpapi/auth.go has the full account).
--
-- The token is the session row's own gen_random_uuid() id, used directly as the cookie
-- value rather than a separately-generated-and-hashed token: it already has 122 bits of
-- randomness from the same source every other id in this schema trusts, and a second
-- hashed-token layer would be defense against a DB read leaking live session tokens, a
-- threat model this personal-scale app isn't defending against yet. Revisit if that changes.
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- Every authenticated request looks up its session by id (the cookie value) directly via
-- the primary key — no additional index needed for that. This one supports the one other
-- real access pattern: invalidating everything for a user (a future "sign out everywhere"),
-- and keeps a `DELETE ... WHERE user_id = $1 ON DELETE CASCADE`-style cleanup cheap.
CREATE INDEX idx_sessions_user ON sessions (user_id);
