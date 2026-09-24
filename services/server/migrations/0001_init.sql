-- IMPLEMENTATION.md §3.1-3.7. See services/server/README.md for why this
-- is split from the jobs table in 0002_jobs.sql.

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- §3.1 users
CREATE TABLE users (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                VARCHAR(255) UNIQUE NOT NULL,
    password_hash        VARCHAR(255),
    privacy_trim_m       INT          NOT NULL DEFAULT 200,  -- endpoint trim; §7. Opt-out.
    last_seen_at         TIMESTAMPTZ,                        -- drives the dormancy policy, §5.7
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    demo_expires_at      TIMESTAMPTZ,   -- §4.10's no-signup demo; NULL for a real account, set
                                        -- for an ephemeral one (internal/worker/demo_purge.go
                                        -- sweeps once past)
    display_name         VARCHAR(255), -- §4.12 account settings
    country              CHAR(2),      -- ISO 3166-1 alpha-2; NULL means unset, defaults the UI
                                        -- to metric units
    avatar_key           TEXT,         -- object storage key; overwritten on re-upload, not
                                        -- accumulated per upload
    avatar_content_type  VARCHAR(64),
    avatar_updated_at    TIMESTAMPTZ
);

CREATE INDEX idx_users_demo_expiry ON users (demo_expires_at) WHERE demo_expires_at IS NOT NULL;

-- §3.2 connections -- Path 1 OAuth state
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
    dedupe_key        TEXT,                    -- see §4.6; cross-source identity

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
    description       TEXT,                    -- §4.7.4; user-editable free text, e.g. for a
                                               -- non-sport GPS trace. NULL means never set.
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Removed: bbox BOX2D column -- PostGIS has no default GiST operator class for box2d, and
-- the GiST index on trajectory already gives an index-backed bounding-box filter.
CREATE UNIQUE INDEX idx_activities_dedupe
    ON activities (user_id, source, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX idx_activities_crosssource ON activities (user_id, dedupe_key);
CREATE INDEX idx_activities_user_time   ON activities (user_id, started_at DESC);
CREATE INDEX idx_activities_spatial     ON activities USING GIST (trajectory);
CREATE INDEX idx_activities_type        ON activities (user_id, activity_type);

-- §3.4 activity_streams -- per-point sensor data, kept out of the hot query path.
CREATE TABLE activity_streams (
    activity_id  UUID PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    point_count  INT NOT NULL,
    -- Parallel arrays, one entry per raw point. Compact, and cheap to slice.
    elapsed_s    INT[],
    elevation_m  REAL[],
    heartrate    SMALLINT[],
    cadence      SMALLINT[],
    power_w      SMALLINT[],
    dist_m       REAL[]   -- cumulative distance at this point; §4.5's pace curves and
                          -- personal bests read the same in-memory array before it's ever
                          -- written here -- see §4.5's closing note on why derived-data reads
                          -- re-parse raw payloads rather than reading this column back out.
);

-- §3.5 user_tiles -- explorer-tile gamification. Not written by this task; table exists
-- for schema completeness per services/server/README.md's migration split.
CREATE TABLE user_tiles (
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom              SMALLINT NOT NULL,   -- 14 (~2.4 km) and 17 (~306 m) at the equator
    tile_x            INT NOT NULL,
    tile_y            INT NOT NULL,
    first_visited_at  TIMESTAMPTZ NOT NULL,
    visit_count       INT NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);

-- §3.6 fog_tiles -- fog rendering is separate, later work; nothing writes to this table yet.
CREATE TABLE fog_tiles (
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom                SMALLINT NOT NULL,
    tile_x              INT NOT NULL,
    tile_y              INT NOT NULL,
    object_key          TEXT,
    heatmap_object_key  TEXT,  -- §4.2.2's additive-intensity raster -- shares this row rather
                               -- than a separate heatmap_tiles table, since both rasters go
                               -- dirty from exactly the same ingest/reprivacy events
    dirty               BOOLEAN NOT NULL DEFAULT TRUE,
    rendered_at         TIMESTAMPTZ,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);

CREATE INDEX idx_fog_dirty ON fog_tiles (user_id) WHERE dirty;

-- §3.7 privacy_zones -- no UI yet; table exists so the
-- endpoint trim in activities ingestion has somewhere to eventually join against.
CREATE TABLE privacy_zones (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    center    GEOGRAPHY(Point, 4326) NOT NULL,
    radius_m  INT NOT NULL DEFAULT 400
);

CREATE INDEX idx_privacy_user ON privacy_zones (user_id);
