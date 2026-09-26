-- Accounts and everything that signs into one: users (IMPLEMENTATION.md §3.1), sessions (§3.9),
-- password_resets (§3.10), email_verifications (§3.16), user_identities (§3.17) and
-- auth_handoffs (§3.18).

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- §3.1 users
CREATE TABLE users (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                VARCHAR(255) UNIQUE NOT NULL,
    password_hash        VARCHAR(255),  -- NULL for an account that only signs in via user_identities
    last_seen_at         TIMESTAMPTZ,   -- drives the dormancy policy, VISION.md §5.7
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    demo_expires_at      TIMESTAMPTZ,   -- §4.10's demo; NULL for a real account. The shared Demo
                                        -- Customer (0006) sets it far in the future: non-NULL keeps
                                        -- it read-only, and internal/worker/demo_purge.go never
                                        -- reaches it
    display_name         VARCHAR(255),  -- §4.12 account settings
    country              CHAR(2),       -- ISO 3166-1 alpha-2; NULL means unset, defaults the UI
                                        -- to metric units
    avatar_key           TEXT,          -- object storage key; overwritten on re-upload, not
                                        -- accumulated per upload
    avatar_content_type  VARCHAR(64),
    avatar_updated_at    TIMESTAMPTZ,
    email_verified       BOOLEAN NOT NULL DEFAULT false,  -- FR-1.8; demo accounts never read it
                                                          -- (httpapi.requireVerified)
    heatmap_cap          DOUBLE PRECISION NOT NULL DEFAULT 8.0,  -- accumulated intensity that maps
                                        -- to full heatmap saturation; kept current by
                                        -- internal/worker/heatmap_cap.go's daily sweep (§4.2.2)
    timezone             TEXT NOT NULL DEFAULT 'UTC',  -- IANA name; every day-bucketing query
                                        -- buckets in it. Resolved client-side at signup, editable
                                        -- in Settings
    is_admin             BOOLEAN NOT NULL DEFAULT false,  -- §4.20; written only by the
                                                          -- `set-admin` CLI subcommand
    locale               VARCHAR(8)     -- §4.21; an internal/i18n catalog code, NULL = follow
                                        -- Accept-Language
);

CREATE INDEX idx_users_demo_expiry ON users (demo_expires_at) WHERE demo_expires_at IS NOT NULL;

-- §3.9 sessions. The token is the row's own gen_random_uuid() id, used directly as the cookie
-- value rather than a separately generated and hashed token: 122 bits from the same source
-- every other id here trusts. A hashed layer would defend against a DB read leaking live
-- tokens, a threat model this app isn't defending against yet. password_resets,
-- email_verifications and auth_handoffs below make the same choice.
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- Lookups by token go through the primary key; this serves "everything for a user".
CREATE INDEX idx_sessions_user ON sessions (user_id);

-- §3.10 password_resets
CREATE TABLE password_resets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_password_resets_user ON password_resets (user_id);

-- §3.16 email_verifications. Longer TTL than a password reset (auth.go's emailVerificationTTL).
CREATE TABLE email_verifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);

-- §3.17 user_identities -- external sign-in identities (FR-1.9, FR-1.10, ADR-0015): Google's
-- id_token "sub", Facebook's app-scoped user id. PRIMARY KEY (provider, subject): one external
-- account maps to exactly one HoldMyTrack account. UNIQUE (user_id, provider): an account links
-- at most one identity per provider.
CREATE TABLE user_identities (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL CHECK (provider IN ('google', 'facebook')),
    subject    VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

-- §3.18 auth_handoffs -- one-time sign-in codes for a native app (FR-1.10, ADR-0016). The
-- browser-tab callback stores a row and redirects to the app with its id; the app redeems it once
-- at POST /v1/auth/handoff with the PKCE verifier for `challenge`, so the id alone is worthless to
-- anyone who intercepts the redirect. Deleted on use; expired rows are purged on each insert.
CREATE TABLE auth_handoffs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge  TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
