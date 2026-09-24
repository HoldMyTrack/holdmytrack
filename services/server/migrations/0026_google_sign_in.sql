-- Sign in with Google (docs/SPEC.md FR-1.9, docs/adr/0009-google-sign-in-server-side-code-flow.md).
-- google_sub is the id_token's "sub" claim — Google's own stable account id, unlike the email
-- address, which a Google account can change. NULL for every account that has never signed in
-- with Google. A plain column rather than a separate identities table: there is exactly one
-- external identity provider, and the ADR records when that stops being enough.
-- UNIQUE because one Google account must map to exactly one HoldMyTrack account.
ALTER TABLE users ADD COLUMN google_sub VARCHAR(255) UNIQUE;
