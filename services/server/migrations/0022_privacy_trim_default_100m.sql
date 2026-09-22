-- Lowers the default privacy trim from 200m to 100m (docs/SPEC.md FR-8.1) — 200m trimmed more
-- off the start/end of every new track than judged necessary. Only changes what a *future*
-- signup starts with (handleSignup's INSERT never lists privacy_trim_m, so it always takes
-- whatever the column default currently is); every existing account keeps whatever value it
-- already has, explicit or previously-defaulted, so there is nothing to backfill here.
ALTER TABLE users ALTER COLUMN privacy_trim_m SET DEFAULT 100;
