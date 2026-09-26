-- IMPLEMENTATION.md §3.8 -- the job queue. Infrastructure the rest of the schema depends on,
-- not part of the activity model, so it has its own file.

CREATE TABLE jobs (
    id          BIGSERIAL PRIMARY KEY,
    kind        VARCHAR(32) NOT NULL,   -- 'ingest' | 'edit_track' | 'reprivacy' | 'render_fog'
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    payload     JSONB NOT NULL,
    state       VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,                   -- English diagnostic, for logs and debugging
    run_after   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_code  VARCHAR(32)             -- ingest.FailureCode; what the apps render, in the
                                        -- reader's language (§4.21)
);

-- The partial index is what makes FOR UPDATE SKIP LOCKED cheap: the queue scan only ever
-- touches runnable rows, not the completed history.
CREATE INDEX idx_jobs_runnable ON jobs (run_after, id) WHERE state = 'pending';
