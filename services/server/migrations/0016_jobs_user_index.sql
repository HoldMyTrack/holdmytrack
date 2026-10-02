-- IMPLEMENTATION.md §3.8: every read of jobs outside the worker's own claim is one account's
-- (the upload history, the Upload menu, a job's status), and jobs keeps its finished history.
CREATE INDEX idx_jobs_user ON jobs (user_id, id);
