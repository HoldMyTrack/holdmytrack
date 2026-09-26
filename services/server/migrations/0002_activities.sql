-- Activities and what hangs off them: activities (IMPLEMENTATION.md §3.3), activity_streams
-- (§3.4), privacy_zones (§3.7) and connections (§3.2).

-- §3.3 activities
CREATE TABLE activities (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Provenance. `source` is the ingest path; `source_detail` names the origin within it
    -- (a provider, an Android package, or an uploaded filename).
    source            VARCHAR(32)  NOT NULL,   -- 'upload' | 'takeout' | 'garmin' | 'wahoo' | 'coros'
                                               -- | 'oura' | 'healthkit' | 'health_connect'
    source_detail     VARCHAR(255),
    external_id       VARCHAR(255),            -- provider activity ID, or HK/HC record UID

    activity_type     VARCHAR(50)  NOT NULL,   -- whatever the source reports, verbatim -- not
                                               -- a controlled vocabulary; see §4.7
    distance_meters   NUMERIC(10,2),
    duration_seconds  INT,                     -- elapsed
    moving_seconds    INT,                     -- excludes stops; the basis for real pace
    elevation_gain_m  NUMERIC(8,2),
    avg_speed_mps     NUMERIC(6,3),
    started_at        TIMESTAMPTZ  NOT NULL,

    -- Display geometry: simplified for rendering. Coverage is NOT derived from this
    -- column -- see §4.1. M dimension carries epoch seconds.
    trajectory        GEOMETRY(LineStringM, 4326),
    raw_payload_key   TEXT,                    -- object storage key for the raw ingest payload:
                                               -- uploaded file (Path 3), provider push (Path 1),
                                               -- or synced point batch (Path 2). See §4.1 step 6.
    description       TEXT,                    -- §4.7.4; user-editable free text. NULL means never set.
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- §4.6 cross-source dedup: the copy that lost points at the one that won, rather than being
    -- deleted, so a user can see why an activity disappeared. ON DELETE SET NULL is deliberate:
    -- deleting the winner puts the copy it displaced back on the map and in the totals.
    superseded_by     UUID REFERENCES activities(id) ON DELETE SET NULL,
    name              VARCHAR(200),            -- §4.7.4; user-entered title, never parsed from a file
    in_heatmap_window BOOLEAN NOT NULL DEFAULT true,  -- §4.2.2; flipped true -> false only by
                                               -- internal/worker/heatmap_aging.go's daily sweep
    track_edit        JSONB,                   -- §4.7.7 Edit track spec, in unix-ms timestamps,
                                               -- replayed over the raw payload; NULL = unedited
    edit_pending      BOOLEAN NOT NULL DEFAULT false  -- true until the `edit_track` job has
                                               -- reprocessed the activity (or failed)
);

-- No bbox column: PostGIS has no default GiST operator class for box2d, and the GiST index on
-- trajectory already gives an index-backed bounding-box filter.
CREATE UNIQUE INDEX idx_activities_dedupe
    ON activities (user_id, source, external_id) WHERE external_id IS NOT NULL;
-- Also serves §4.6's overlap lookup, a range scan on (user_id, started_at), and the reads that
-- span superseded rows too (delete, the duplicates listing).
CREATE INDEX idx_activities_user_time   ON activities (user_id, started_at DESC);
-- Every other user-facing read is "this user's live activities, newest first".
CREATE INDEX idx_activities_live        ON activities (user_id, started_at DESC)
    WHERE superseded_by IS NULL;
CREATE INDEX idx_activities_spatial     ON activities USING GIST (trajectory);
CREATE INDEX idx_activities_type        ON activities (user_id, activity_type);
-- heatmap_aging.go's "still-in-window rows old enough to have aged out".
CREATE INDEX idx_activities_in_heatmap_window ON activities (started_at) WHERE in_heatmap_window;

-- §3.4 activity_streams -- per-point sensor data, kept out of the hot query path.
CREATE TABLE activity_streams (
    activity_id  UUID PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    point_count  INT NOT NULL,
    -- Parallel arrays, one entry per raw point. Compact, and cheap to slice.
    elapsed_s    INT[],
    elevation_m  REAL[],
    heartrate    SMALLINT[],
    dist_m       REAL[]   -- cumulative distance at this point
);

-- §3.7 privacy_zones -- shown to users as Private locations (FR-8.1, ADR-0010): ingest drops the
-- leading and trailing points inside one.
CREATE TABLE privacy_zones (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    center      GEOGRAPHY(Point, 4326) NOT NULL,
    radius_m    INT NOT NULL DEFAULT 200,
    name        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_privacy_user ON privacy_zones (user_id);

-- §3.2 connections -- Path 1 OAuth state. Nothing writes it yet.
CREATE TABLE connections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider          VARCHAR(32) NOT NULL,   -- 'garmin' | 'wahoo' | 'coros' | 'oura'
    provider_user_id  VARCHAR(255) NOT NULL,
    access_token      BYTEA NOT NULL,         -- encrypted at rest, never logged
    refresh_token     BYTEA,
    expires_at        TIMESTAMPTZ,
    scopes            TEXT[],
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_synced_at    TIMESTAMPTZ,
    last_record_at    TIMESTAMPTZ,            -- watermark: newest activity already ingested
    UNIQUE (user_id, provider),
    UNIQUE (provider, provider_user_id)       -- one provider account maps to one HoldMyTrack user
);
