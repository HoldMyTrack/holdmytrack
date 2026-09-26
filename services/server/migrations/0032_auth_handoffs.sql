-- One-time sign-in codes for a native app (docs/SPEC.md FR-1.10,
-- docs/adr/0016-native-sign-in.md). The Android app runs Facebook's web sign-in in a browser
-- tab; the callback, instead of setting a session cookie in that tab, stores a row here and
-- redirects to the app with its id. The app redeems it once, at POST /v1/auth/handoff, with the
-- PKCE verifier whose S256 challenge it sent when it started — so the id alone is worthless to
-- anyone who intercepts the redirect. Deleted on use; expired rows are purged on each insert.
CREATE TABLE auth_handoffs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge  TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
