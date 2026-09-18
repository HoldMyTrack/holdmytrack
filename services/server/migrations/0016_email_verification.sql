-- Email verification (docs/ROADMAP.md's "Email verification + demo without real ingest") —
-- real accounts now hold on an unverified state until they click through, so a mistyped
-- signup email can be caught (and corrected via PATCH /v1/auth/email) before any Sync work
-- is ever done against an account nobody can get back into. Demo accounts (demo_expires_at
-- IS NOT NULL) never look at this column at all — see httpapi.requireVerified.
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

-- Same shape as password_resets (0008): the row's own gen_random_uuid() primary key is the
-- verification token, no separate hashing layer, for the same reasoning that decision already
-- carries. TTL is longer than a password reset's (see auth.go's emailVerificationTTL) — a
-- signup confirmation is less time-sensitive than a credential reset.
CREATE TABLE email_verifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);
