-- Account deletion (IMPLEMENTATION.md §4.29, SPEC FR-1.11). Deleting an account closes it at
-- once — sessions, identities and tokens gone, its email freed — and stamps deleted_at;
-- internal/worker/account_purge.go then removes its stored objects and the row itself, whose
-- cascade takes everything else.
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX idx_users_deleted ON users (deleted_at) WHERE deleted_at IS NOT NULL;
