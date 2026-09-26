-- External sign-in identities (docs/SPEC.md FR-1.9, FR-1.10;
-- docs/adr/0015-identities-table-and-facebook-sign-in.md). One row per (provider, the
-- provider's own stable account id) — Google's id_token "sub", Facebook's app-scoped user id —
-- replacing 0026's users.google_sub now that Facebook makes it two providers.
-- PRIMARY KEY (provider, subject): one external account maps to exactly one HoldMyTrack
-- account. UNIQUE (user_id, provider): an account links at most one identity per provider.
CREATE TABLE user_identities (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL CHECK (provider IN ('google', 'facebook')),
    subject    VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

INSERT INTO user_identities (user_id, provider, subject)
SELECT id, 'google', google_sub FROM users WHERE google_sub IS NOT NULL;

ALTER TABLE users DROP COLUMN google_sub;
