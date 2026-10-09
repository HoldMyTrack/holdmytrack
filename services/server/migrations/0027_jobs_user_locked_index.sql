-- IMPLEMENTATION.md §3.8: the worker's claim orders accounts by when each was last served —
-- its latest locked_at (internal/worker's claimQuery) — read once per candidate account.
CREATE INDEX idx_jobs_user_locked ON jobs (user_id, locked_at);
