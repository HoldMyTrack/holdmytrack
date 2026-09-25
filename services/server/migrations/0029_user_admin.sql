-- The admin panel (IMPLEMENTATION.md §4.20, SPEC.md FR-12, ADR-0013). An account with
-- is_admin can open /admin; every other account gets the ordinary 404 page there. Granted and
-- revoked only by the `set-admin` CLI subcommand — no HTTP route writes this column.
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;
