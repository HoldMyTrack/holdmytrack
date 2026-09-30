-- The header's Upload menu and the /sync page (IMPLEMENTATION.md §4.0.1): when an import job
-- finished, and when its account last looked at its finished imports — a failure after that
-- is one the Upload menu still flags.

ALTER TABLE jobs ADD COLUMN finished_at TIMESTAMPTZ;   -- set by the worker on 'done' or 'failed'
UPDATE jobs SET finished_at = created_at WHERE state IN ('done', 'failed');

-- NOW() for existing accounts, so no failure from before this column existed reads as unseen.
ALTER TABLE users ADD COLUMN imports_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
