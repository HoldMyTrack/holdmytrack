-- IMPLEMENTATION.md §3.8. Split from 0001_init.sql on purpose: the job
-- queue is infrastructure the rest of the schema depends on, not part of the
-- activity-tracking model. See services/server/README.md.

CREATE TABLE jobs (
    id          BIGSERIAL PRIMARY KEY,
    kind        VARCHAR(32) NOT NULL,   -- 'ingest' | 'render_fog' | 'render_export'
                                        -- | 'reprivacy' | 'provider_sync' | 'retention'
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    payload     JSONB NOT NULL,
    state       VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    run_after   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The partial index is what makes FOR UPDATE SKIP LOCKED cheap: the queue scan only ever
-- touches runnable rows, not the completed history.
CREATE INDEX idx_jobs_runnable ON jobs (run_after, id) WHERE state = 'pending';
