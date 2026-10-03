-- Downloading your data (IMPLEMENTATION.md §4.29, SPEC FR-1.12). One row per request: the
-- worker's `export` job (job_id) builds the archive as zip parts under
-- exports/{user_id}/{id}/{n}.zip and sets ready_at, expires_at and part_sizes; the worker's
-- sweep removes the parts and the row once expires_at has passed. Until ready_at, where the
-- request stands is its job's state.
CREATE TABLE exports (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id        BIGINT REFERENCES jobs(id) ON DELETE SET NULL,
    lang          VARCHAR(8) NOT NULL,   -- the language of the request, for the email and README
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ready_at      TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    part_sizes    BIGINT[]               -- bytes of each part, in order
);

CREATE INDEX idx_exports_user ON exports (user_id, requested_at DESC);
CREATE INDEX idx_exports_expiry ON exports (expires_at) WHERE expires_at IS NOT NULL;
