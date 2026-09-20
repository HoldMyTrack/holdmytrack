# FitMap: Implementation

> **Scope note.** FitMap ingests activities; it never records them. There is no in-app GPS capture, and no social graph — see `VISION.md` §1.1 and §5.6 for why both are out of scope. This document covers the database schema and each feature's own implementation detail (ingest, storage, analysis, fog rendering, tile serving) — see `docs/ARCHITECTURE.md` first for the system-level shape and the stack this all runs on.

---

## 3. Database Schema

### 3.1 `users`

```sql
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
```

**No `tier` column.** There are no tiers; every account has every feature (`VISION.md` §6.2). `last_seen_at` exists because storage is the cost that grows forever and dormant accounts are the largest recoverable share of it. `demo_expires_at` (§4.10's no-signup demo) means a demo account is a real row in this same table, not a separate mechanism. `display_name`/`country`/`avatar_key`/`avatar_content_type`/`avatar_updated_at` back §4.12's account settings page.

### 3.2 `connections` — Path 1 OAuth state

```sql
CREATE TABLE connections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider          VARCHAR(32) NOT NULL,   -- 'garmin' | 'wahoo' | 'coros'
    provider_user_id  VARCHAR(255) NOT NULL,
    access_token      BYTEA NOT NULL,         -- encrypted at rest, never logged
    refresh_token     BYTEA,
    expires_at        TIMESTAMPTZ,
    scopes            TEXT[],
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_synced_at    TIMESTAMPTZ,
    last_record_at    TIMESTAMPTZ,            -- watermark: newest activity already ingested
    UNIQUE (user_id, provider),
    UNIQUE (provider, provider_user_id)       -- one provider account maps to one FitMap user
);
```

The second unique constraint matters: without it, two FitMap accounts can connect the same Garmin account and each receive the same webhook, doubling both storage and fog.

Tokens are encrypted at rest with a key outside the database. Deauthorization deletion (§7) is a hard requirement of all three activity providers, so disconnecting must delete the synced data, not merely the row here.

### 3.3 `activities`

```sql
CREATE TABLE activities (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Provenance. `source` is the ingest path; `source_detail` names the origin within it
    -- (a provider, an Android package, or an uploaded filename).
    source            VARCHAR(32)  NOT NULL,   -- 'upload' | 'takeout' | 'garmin' | 'wahoo'
                                               -- | 'coros' | 'healthkit' | 'healthconnect'
    source_detail     VARCHAR(255),
    external_id       VARCHAR(255),            -- provider activity ID, or HK/HC record UID
    -- The copy that displaced this one, or NULL when this row is live (§4.6). ON DELETE SET
    -- NULL, so deleting the winner puts the copy it displaced back on the map rather than
    -- orphaning it into permanent invisibility.
    superseded_by     UUID REFERENCES activities(id) ON DELETE SET NULL,

    activity_type     VARCHAR(50)  NOT NULL,   -- whatever the source reports, verbatim —
                                               -- not a controlled vocabulary; see §4.7
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
    description       TEXT,                    -- §4.7.4; user-editable free text, NULL means
                                               -- never set. Never parsed from a GPX/TCX/FIT
                                               -- file or from Health Connect/HealthKit sync;
                                               -- Path 2's JSON wire format is the one ingest
                                               -- path that can set it at creation (§4.0.4,
                                               -- in-app GPS recording).
    name              VARCHAR(200),            -- §4.7.4/migrations/0015; user-editable title,
                                               -- shown in place of started_at when set. Same
                                               -- "never at ingest except §4.0.4" rule as
                                               -- description above. NULL means never set.
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_activities_dedupe
    ON activities (user_id, source, external_id) WHERE external_id IS NOT NULL;
-- Every user-facing read is "this user's live activities", so that is the index they get.
CREATE INDEX idx_activities_live ON activities (user_id, started_at DESC)
    WHERE superseded_by IS NULL;
-- §4.6's collision lookup: same user, same type, a range around a start time.
CREATE INDEX idx_activities_dedupe_window ON activities (user_id, activity_type, started_at);
CREATE INDEX idx_activities_user_time   ON activities (user_id, started_at DESC);
CREATE INDEX idx_activities_spatial     ON activities USING GIST (trajectory);
CREATE INDEX idx_activities_type        ON activities (user_id, activity_type);
```

**Removed: the `bbox BOX2D` column and its index.** `CREATE INDEX … USING GIST(bbox)` on a `BOX2D` column fails — PostGIS provides no default GiST operator class for `box2d`. It was also redundant: the GiST index on `trajectory` is a bounding-box index, so `trajectory && ST_TileEnvelope(...)` already gets an index-backed viewport filter.

### 3.4 `activity_streams`

Per-point sensor data, kept out of the hot query path so `activities` stays narrow. **This is where the per-activity pace/heart-rate profile (§4.5) actually lives.**

```sql
CREATE TABLE activity_streams (
    activity_id  UUID PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    point_count  INT NOT NULL,
    -- Parallel arrays, one entry per raw point. Compact, and cheap to slice.
    elapsed_s    INT[],
    elevation_m  REAL[],
    heartrate    SMALLINT[],
    dist_m       REAL[]   -- cumulative distance at this point.
);
```

`cadence`/`power_w` columns existed here (migrations 0001–0012) but were never read back by any query, API response, or client — dropped in migration 0013 as dead weight. FitMap's scope is outdoor GPS tracking (`VISION.md` §1.1), not a sports-computer sensor product; `heartrate` and `elevation_m` stay because they feed the track-metrics pace/HR profile.

**This table is the single largest storage line in the product** (`VISION.md` §4.3), so it is also the first place §5.7's retention policy applies.

Availability varies by path: Path 1 and Path 3 deliver full sensor data — including GPX, since `parse/gpx.go` reads the `gpxtpx:TrackPointExtension` block for heart rate. Path 2 delivers partial data, since heart rate is a separate record type with its own permissions on both platforms.

### 3.5 `user_tiles`

Explorer-tile gamification. Slippy tiles rather than H3: they align 1:1 with the MVT/raster grid, rollups are an integer shift, and z14/z17 is the vocabulary the community already uses.

```sql
CREATE TABLE user_tiles (
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom              SMALLINT NOT NULL,   -- 14 (~2.4 km) and 17 (~306 m) at the equator
    tile_x            INT NOT NULL,
    tile_y            INT NOT NULL,
    first_visited_at  TIMESTAMPTZ NOT NULL,
    visit_count       INT NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);
```

These are the gamification units. They are **not** the fog resolution — see §4.2.

### 3.6 `fog_tiles`

```sql
CREATE TABLE fog_tiles (
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    zoom              SMALLINT NOT NULL,
    tile_x            INT NOT NULL,
    tile_y            INT NOT NULL,
    object_key        TEXT,   -- the fog (coverage) raster
    heatmap_object_key TEXT,  -- §4.2.2 — the additive-intensity raster, added here rather
                              -- than a separate heatmap_tiles table because the two go
                              -- dirty together, from the same ingest events, every time
    dirty             BOOLEAN NOT NULL DEFAULT TRUE,
    rendered_at       TIMESTAMPTZ,
    PRIMARY KEY (user_id, zoom, tile_x, tile_y)
);

CREATE INDEX idx_fog_dirty ON fog_tiles (user_id) WHERE dirty;
```

### 3.7 `privacy_zones`

```sql
CREATE TABLE privacy_zones (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    center    GEOGRAPHY(Point, 4326) NOT NULL,
    radius_m  INT NOT NULL DEFAULT 400
);

CREATE INDEX idx_privacy_user ON privacy_zones (user_id);
```

### 3.8 `jobs`

The queue §2 and §4.1 both depend on.

```sql
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
```

### 3.9 `sessions`

Added with §4.9's real accounts (`migrations/0006_sessions.sql`) — appended here rather than renumbered in among 3.1–3.8 so every existing cross-reference to those numbers elsewhere in this document stays correct.

```sql
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_sessions_user ON sessions (user_id);
```

**The row's own primary key is the session cookie's value** — no separate hash-of-token layer. Judged sufficient for this app's current single-user threat model (§4.9); revisit the day that changes. Expiry is enforced in SQL (`expires_at > NOW()`) at read time, not left to the browser dropping an expired cookie on its own.

### 3.10 `password_resets`

Added with §4.11's password recovery (`migrations/0008_password_resets.sql`) — same shape and reasoning as `sessions` above, just for a much shorter-lived token.

```sql
CREATE TABLE password_resets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_password_resets_user ON password_resets (user_id);
```

### 3.11 `activity_tile_masks`

Added with §4.2.3's filter-aware fog/heatmap revision (`migrations/0005_activity_tile_masks.sql`) — one crisp, unblurred contribution mask per activity per z14 tile it touches, rather than one aggregate raster per user. This is what lets fog/heatmap answer a date-range or hidden-activity filter by compositing a handful of small pre-rendered masks instead of re-parsing raw GPS files per request.

```sql
CREATE TABLE activity_tile_masks (
    activity_id      UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    zoom             SMALLINT NOT NULL DEFAULT 14, -- always 14 today; kept for schema symmetry
                                                    -- with fog_tiles
    tile_x           INT NOT NULL,
    tile_y           INT NOT NULL,
    mask_object_key  TEXT NOT NULL,
    rendered_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (activity_id, zoom, tile_x, tile_y)
);

CREATE INDEX idx_activity_tile_masks_tile ON activity_tile_masks (zoom, tile_x, tile_y);
```

`fog_tiles` (§3.6) is unchanged in shape and meaning — it stays the cached "everyone, whole history, nothing hidden" composite. What changed is only how that composite (and a filtered one) gets computed: by compositing these per-activity masks, not by re-parsing raw payloads.

### 3.12 `admin_countries`

Natural Earth's 1:50m Admin-0 country polygons, loaded once by the `seed-admin-boundaries` subcommand (§4.2.4) and never derived from user data. `adm0_a3` — Natural Earth's own stable per-country code — is the seed's idempotency key, not `iso_a2`: a few small dependencies share their parent country's ISO code (Australia's Indian Ocean Territory and Ashmore & Cartier Islands both carry `AU`), so `iso_a2` is nullable and intentionally not unique, informational only, never joined on.

```sql
CREATE TABLE admin_countries (
    id       SERIAL PRIMARY KEY,
    adm0_a3  TEXT NOT NULL UNIQUE,
    iso_a2   CHAR(2),
    name     TEXT NOT NULL,
    geom     GEOMETRY(MultiPolygon, 4326) NOT NULL
);

CREATE INDEX idx_admin_countries_geom ON admin_countries USING GIST (geom);
```

### 3.13 `admin_regions`

Natural Earth's 1:10m Admin-1 (state/province) polygons — 1:10m, not 1:50m, because Natural Earth's own 1:50m Admin-1 export only covers 9 of 242 countries (Russia, the US, India, Indonesia, China, Brazil, Canada, Australia, South Africa); the 1:10m export covers every one. Confirmed directly against the vendored data: every `admin_countries` row has at least one `admin_regions` row, even single-region sovereign states like Monaco or Vatican City, so no synthetic "whole country as one region" row is ever needed. `adm1_code` — again Natural Earth's own stable code, globally unique and already prefixed with its country's `adm0_a3` — is the seed's idempotency key; `code` holds ISO 3166-2 where Natural Earth has one and is NULL otherwise.

```sql
CREATE TABLE admin_regions (
    id         SERIAL PRIMARY KEY,
    country_id INT NOT NULL REFERENCES admin_countries(id),
    adm1_code  TEXT NOT NULL UNIQUE,
    code       TEXT,
    name       TEXT NOT NULL,
    geom       GEOMETRY(MultiPolygon, 4326) NOT NULL
);

CREATE INDEX idx_admin_regions_geom ON admin_regions USING GIST (geom);
CREATE INDEX idx_admin_regions_country ON admin_regions (country_id);
```

A handful of the 1:10m export's rows (16 of 4,596, confirmed live) carry an `adm0_a3` with no matching `admin_countries` row at all — disputed micro-territories the coarser 1:50m country layer drops (Gibraltar, Bir Tawil, and similarly small cases). The seed step skips these and logs a count rather than failing the whole load; there is no `country_id` to attach them to.

### 3.14 `activity_country` / 3.15 `activity_region`

One row per (activity, country/region) its trajectory touches — computed once by `internal/geo.MatchActivity`, called from `ingest.Process` right after the activity is persisted (§4.2.4), and backfilled once for pre-existing activities by `seed-admin-boundaries`. This is what lets the Country/Region tile queries do a cheap indexed lookup instead of the geometry test itself at request time (ADR-0008).

```sql
CREATE TABLE activity_country (
    activity_id UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    country_id  INT NOT NULL REFERENCES admin_countries(id),
    PRIMARY KEY (activity_id, country_id)
);

CREATE INDEX idx_activity_country_country ON activity_country (country_id);

CREATE TABLE activity_region (
    activity_id UUID NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    region_id   INT NOT NULL REFERENCES admin_regions(id),
    PRIMARY KEY (activity_id, region_id)
);

CREATE INDEX idx_activity_region_region ON activity_region (region_id);
```

`ON DELETE CASCADE` on `activity_id` means deleting an activity needs no matching cleanup code elsewhere: its membership rows disappear with it, and since the tile queries read this table live rather than a cached aggregate, the country/region re-locks on the very next request with nothing to invalidate.

Matching runs against `activities.trajectory` — the already-persisted, simplified display geometry — not the raw pre-simplification points `activity_tile_masks` uses. Country/region polygons are kilometers across, so `simplifyToleranceDeg`'s ~3 m tolerance cannot plausibly change which one a segment intersects, and reading the already-persisted column avoids a second geometry pass in Go. Every polygon touched gets a row however briefly the trajectory crossed it, matching that a visit's size or duration doesn't matter, only whether it happened; rows are not filtered by `superseded_by`, mirroring `activity_tile_masks`'s own reasoning — a deleted winner makes a superseded duplicate's coverage live again for free — with the filtering done instead at read time by the tile queries themselves.

---

## 4. Core Technical Workflows

### 4.0 The three ingest paths

They differ only in how bytes arrive. All converge on §4.1 step 2.

**Path 1 — cloud-to-cloud.** OAuth connect, then webhook-driven where the provider supports it. Never poll on a schedule: a webhook returns `200 OK` in milliseconds and enqueues a `provider_sync` job; nothing is fetched inline. Providers deliver either a file (Garmin pushes `.FIT`) or structured JSON, so the parse step is per-provider and everything after it is shared. Backfill of history at connect time is a separate, rate-limited, resumable job — it is the largest single fetch the system ever performs.

**Path 2 — on-device sync.** A native app reads the platform health store and uploads normalized points.

* **iOS/HealthKit** exposes `HKWorkoutRoute`; full geometry is available to a native app.
* **Android/Health Connect** is constrained in three ways, each verified on a device rather than taken from documentation: `READ_EXERCISE_ROUTES` cannot be requested programmatically (the user grants it manually in Health Connect settings); **routes written by other apps cannot be read in the background** — `ExerciseRouteResult.ConsentRequired` is returned even with "Always allow"; and **only the last 30 days are readable at all** unless `READ_HEALTH_DATA_HISTORY` is granted, which an app built around accumulated history has to ask for (it is a time window over data types already declared, not a further data type). Android sync is therefore **foreground-only**, and for **Samsung Health specifically, route geometry is not exposed at all**, so Galaxy Watch sync yields summary metrics and no map.

  Do not build a background route sync for Android; it cannot work. A sync that reliably fetches everything except the map is worse than none, because it advances the watermark past records whose routes were never read.

**Path 3 — file upload.** `.GPX`, `.FIT`, `.TCX`, and bulk-export archives (a Strava export zip is thousands of files). Unconditional: no API terms, no licence, no permission model. This path also serves the **no-signup demo** (§4.10, §7) — an ephemeral real account behind the scenes, so this same path runs completely unchanged for a demo visitor; nothing here has a demo-specific branch.

```http
POST /v1/activities/upload        # multipart, Path 3
POST /v1/sync/activities          # batched normalized points, Path 2
POST /v1/webhooks/{provider}      # Path 1
```

All three are idempotent on `(user_id, source, external_id)`, and this is a hard invariant, not an optimization: whatever arrives twice from any source — a re-uploaded file, a redelivered webhook, a re-synced batch — must be processed at most once, not once per arrival. Retries are normal, not exceptional — mobile uploads get interrupted and providers redeliver webhooks. The guarantee has to hold under concurrent duplicates, not just sequential ones: a receive-time existence check (§4.1 step 1) is a fast path, not the enforcement. The actual guarantee is `idx_activities_dedupe` (§3.3) plus an upsert-on-conflict at persist time (§4.1 step 6), so two copies of the same activity racing through two separate jobs still land as one row.

#### 4.0.1 Upload UI: batching, non-blocking status, and a persistent history

**Built.** The single-file `UploadWidget.tsx` (drag one, or click to pick one, watch one hover popover) is gone, replaced entirely by `UploadPanel.tsx` — a dropdown panel opened from the header's "Upload activity" button.

- **Both entry points stay, and both go multi-file**, as decided. The drop zone and a `<input type="file" multiple>` both call the same `enqueueFiles`, which accepts every file handed to it rather than the old `files[0]`-only behavior.
- **Up to `MAX_PLAIN_FILES` (20) individually-selected files** per batch, exactly as decided — a batch over that is rejected client-side in full (not silently truncated to 20) with an inline notice: *"Too many files selected (N) — zip them and upload the archive instead."*
- **`.zip` archives bypass the 20-file limit**, extracted and ingested server-side (`handleZipUpload`, `services/server/internal/httpapi/zip_upload.go`) — one `ingest` job per contained `.gpx`/`.fit`/`.tcx` file, reusing the exact same `persistAndEnqueue` a plain single-file upload calls, so a file is handled identically whether it arrived alone or inside an archive. Bounds, per §5.1's zip-bomb defense: `maxZipUploadBytes` (512 MiB, the archive's own compressed size), `maxZipEntries` (5000, a coarse cap on how many files one request processes), `maxZipEntryBytes` (64 MiB per contained file, the same ceiling a plain upload already has) — checked against each entry's *declared* size before decompressing anything, and again against what a bounded read actually returns, since a declared size is exactly what a hostile or corrupt archive can lie about. One bad entry (wrong extension, oversized, unreadable) is recorded as `"skipped"` with a reason and the rest of the batch proceeds — confirmed live: a zip containing two valid `.gpx` files and one `.txt` enqueued the two and reported the third skipped with `unsupported file type ".txt"`, in one response.
- **Never blocking**, as decided — no modal, no locked UI during transfer or server-side processing. Each file uploads via its own request and updates its own row.
- **The Activities panel/totals/histogram/tracks-layer refresh (`onUploaded`) needs a second trigger, not just the upload request resolving.** The upload request resolving only confirms the job was *enqueued* — the worker hasn't parsed the file or inserted its `activities` row yet at that point, so firing `onUploaded` only then would leave a freshly uploaded track missing from the Activities list (and the map) until a manual reload. `useUploadHistory` already polls `GET /v1/uploads` every 1.5s while anything is processing, purely to flip that upload row from "Processing" to "Ready" — `onUploaded` fires on every one of those poll ticks too, which is the one signal that reliably catches a job actually finishing.
- **Live status and the persistent history are the same list, not two separate things** — this is the one real resolution beyond what was decided rather than a straightforward implementation of it. The plan's "live status (per-file popover rows) vs. a persistent history (a separate `GET /v1/uploads` view)" turned out to collapse into one surface once `GET /v1/uploads` existed: a freshly enqueued job is *immediately* a row in that endpoint's response (state `pending` → `"processing"`), so there is no separate in-flight status to show beyond what the history list already reflects, except the network-transfer phase itself — its own progress bar comes from `XMLHttpRequest` directly (`uploadFile` in `api.ts`, the one place in this codebase that isn't `fetch`, since `fetch` has no cross-browser-reliable upload-progress event) — which happens *before* a job even exists to be listed. `useUploadHistory.ts` polls the currently-viewed page of `GET /v1/uploads` every 1.5s while its `processing` count is nonzero, so a row visibly flips from "Processing…" to "Ready"/"Failed" without the panel needing to be closed and reopened. This also means no separate per-batch toast: the panel itself, opened, *is* the live view, and a closed panel's header badge (in-flight count) is the passive signal — resolving "open: where the history view lives" toward one single combined drop-down panel, decided directly rather than left open.
- **Explicitly not WebSockets or SSE**, as decided — `useUploadHistory`'s polling is exactly that, just polling a list endpoint instead of one status endpoint per file. `GET /v1/activities/status/{external_id}` (§4.1's single-item lookup) still exists, but the frontend no longer calls it now that the list endpoint's own `state` covers the same need for every upload at once.
- **The persistent upload history**, built as `GET /v1/uploads?limit=&offset=` (`services/server/internal/httpapi/uploads.go`) — `LIMIT`/`OFFSET` pagination (5 per page), not the keyset pagination §4.7's histogram uses, since this is genuinely the one place in the upload flow pagination is justified the way §4.7's own list dropped it. Reads the `jobs` table directly (`kind = 'ingest'`) rather than a dedicated uploads table — every upload already is an `ingest` job, and `jobs` already carries filename (`payload->>'source_detail'`), external id, state, error and submission time. A `LEFT JOIN` against `activities` on `(user_id, source, external_id)` — `source` read from the job's own payload, not hardcoded to `'upload'` (hardcoding it would leave every Google Takeout import, §4.0.2's `source = 'takeout'`, stuck showing "Ready" with no date or distance, since the join would never match) — is what turns a `"done"` row into "9 Sep · 34.7 km" rather than a bare status. A second cheap query (`uploadsProcessingCountQuery`) returns the *global* pending-job count regardless of which page is open — the header badge and the polling decision above both need the true number, not an artifact of whichever page happens to be on screen. Cross-checked directly: uploaded a small batch, confirmed the response's `total`/`processing` counts and each row's status/date/distance against the `jobs`/`activities` tables by hand before wiring the frontend to it.

#### 4.0.2 Google Takeout import

**Built.** A Google Health / Fitbit Takeout export imports through the exact same `POST /v1/activities/upload` endpoint as any other `.zip` — detected automatically (`isTakeoutArchive`, `services/server/internal/httpapi/takeout_upload.go`), not a separate upload path a user has to choose.

**The real shape, found by inspecting an actual ~2000-file, 1.9 GB export, not assumed from Google's own documentation of it (there isn't much, and what exists drifts from reality — the export's own `UserExercises README.txt` describes a schema the archive doesn't actually contain).** There are no `.gpx`/`.fit`/`.tcx` files anywhere in a real export. GPS points live in `Physical Activity_GoogleData/gps_location_YYYY-MM-DD.csv`, one file per calendar day, at roughly 1 Hz; the activities themselves — which say that a particular stretch of one of those days was a walk — live in a separate `Global Export Data/exercise-*.json` family. Neither is any use without the other, and joining them correctly is real, already-solved work: most days carry two overlapping recording devices (a phone and a watch logging the same walk a few meters apart, which must be deduped by keeping whichever saw more of the activity, not concatenated), the exercise logs' own timestamps carry no UTC offset and have not always been UTC, and an activity window can run past midnight, spanning two day files.

**Shells out to `pathify` rather than reimplementing that join in Go.** `pathify` (`github.com/np25071984/pathify`) is a separate, MIT-licensed Rust CLI, same author, built specifically to read this export shape — `pathify takeout`'s own README and 1.4.0 `CHANGELOG.md` entry describe exactly the device-dedup, UTC-offset self-correction, and midnight-stitching logic above, already tested and shipped. Reimplementing that in Go would duplicate real, tricky, already-solved work for no benefit. Two additions were requested and shipped in pathify 1.4.0 specifically to make this integration possible: `takeout --list --json` (a machine-readable version of the type/GPS-count listing, for a non-interactive caller deciding what to ask for) and `takeout --per-activity` (writes one output file per activity into a directory — `<UTC-start>-<type-slug>.gpx` — instead of welding every match into one combined multi-track file). That second one is the load-bearing design choice: it means FitMap's own ingest pipeline never has to learn about multi-activity files at all. `internal/parse/gpx.go` and `tcx.go` both assume one file is one activity (confirmed directly — a multi-`<trk>` GPX or multi-`<Activity>` TCX gets silently merged into one activity with last-value-wins semantics today), and `ingest.Process` persists exactly one `activities` row per job; extending any of that to fan out one file into several would have been real, risky surgery on code every other ingest path also depends on. Drawing the one-file-one-activity boundary once, correctly, in pathify — which already knows each activity's exact time window — avoids that entirely.

**`handleTakeoutUpload`'s actual sequence**, once `isTakeoutArchive` recognizes the zip (by the presence of a `Takeout/Google Health/` entry prefix, confirmed against the real sample): write the uploaded bytes to a temp file (`pathify` needs a real path, same reason `zip.NewReader` needs an `io.ReaderAt` rather than the raw HTTP body); run `pathify takeout <path> --list --json` and keep only types with `with_gps > 0` — a `Workout`, `Swim` or `Rowing machine` type present in the real sample never had GPS to begin with, and pathify's own stance on that is exactly right ("a swim with no coordinates is not something anyone can hand you as a track"); for each such type, run `pathify takeout <path> --type <name> --per-activity -o <type-dir>` into its own temp directory (one invocation per type, not one combined invocation across every type — pathify's `--per-activity` output has no way to report back which file came from which requested type when several are asked for at once, and a per-type directory split gives that for free with no filename-parsing needed); then persist every file in that directory through the exact same `persistAndEnqueue` a plain upload or a zip entry already uses, with `Source: "takeout"` and `ActivityType` set to the type name that directory was extracted for.

**`ingest.Job` gained an `ActivityType` override field for exactly this.** A bare per-activity GPX pathify writes carries no `<type>` element (confirmed directly — it has a `<name>` like "Walk 2026-05-24", not a `<trk><type>`), so the parser alone would report every one of them `"unknown"`. Rather than teach `ParseGPX` to recover a type from a composed display string (fragile, and would apply pathify's own naming convention to every GPX source, not just this one), `handleTakeoutUpload` already knows the correct type — it's the directory this file came from — and threads it straight through `ingest.Job.ActivityType`, applied by `ingest.Process` right after parsing, overriding whatever the parser found. Every other caller leaves it empty, which is a no-op (the zero value already does the right thing).

**One archive per request; a multi-part export isn't stitched.** Google splits an export larger than the chosen maximum size into `…-001.zip`, `…-002.zip`, and so on, and pathify's own CLI accepts several archives (or an unzipped directory) at once specifically so the exercise logs and the GPS days — which can land in different parts — are read together. This server reads exactly one uploaded zip per request today. Not built, deliberately: the one real sample available (1.9 GB, ~2000 files) was self-contained despite its `…-1-001.zip` name, and adding multi-part-request stitching ahead of a real archive that actually needs it would be speculative complexity per §1.3's own pattern.

**Explicitly not built: importing the no-GPS activities.** `Workout`, `Swim`, `Rowing machine` and similar carry real metrics of their own — distance, calories, heart rate, duration — in the exercise records pathify skips (by design) because there's no route to draw. Importing those as metrics-only, no-trajectory activities is a separate piece of work, reading `Global Export Data/exercise-*.json` or `Health Fitness Data_GoogleData/ UserExercises_*.csv` directly rather than anything to do with pathify, and reusing the schema and UI treatment a null-`trajectory` row already gets end-to-end (`bbox: null`, `ActivitiesPanel.tsx`'s "No track recorded for this activity"). Worth a future pass of its own; not bundled into this one.

**Docker**: `services/server/Dockerfile` gained a `rust:1.88-bookworm` builder stage (`cargo install pathify-cli --version 1.4.0 --locked`, pinned deliberately — bumping it is a decision to make on purpose, not drift), and the final stage moved from `distroless/static-debian12` to `distroless/cc-debian12`: `fitmap` itself is a static Go binary either image would run, but `pathify` is a normal dynamically-linked Rust binary and needs libc — the `cc` distroless variant provides that while keeping the same no-shell, no-package-manager posture the `static` variant had.

**Verified against the real sample**, not synthetic fixtures: uploaded `docs/takeout-20260909T182813Z-1-001.zip` through the live `POST /v1/activities/upload` endpoint, confirmed 118 jobs enqueued matching `--list --json`'s own `total_with_gps` exactly (117 Walk, 1 Bike), all 118 reached `state = 'done'` with zero failures, and cross-checked one activity's persisted `distance_meters`/`duration_seconds`/`started_at` by hand against `pathify info` run directly on the same extracted file — the persisted numbers were correctly *smaller* than pathify's raw ones and `started_at` correctly *later*, exactly matching what §7's existing 200 m endpoint privacy trim is supposed to do to both ends of a track, not a discrepancy.

#### 4.0.3 `POST /v1/sync/activities` — Path 2's batched sync endpoint

**Built.** `internal/httpapi/sync_activities.go`. Takes a JSON body `{source, activities: [{external_id, activity_type, points: [{lat, lon, elevation_m, time, heart_rate}], name, description}]}` and returns one result per activity (`"enqueued"` / `"already_processed"` / `"rejected"`, with an error string on rejection) rather than a single pass/fail for the whole request — a batch is not all-or-nothing, the same "one bad entry doesn't abort the rest" treatment §4.0.1's zip upload already gives a mixed-quality archive. `source` is checked against an allowlist (`"healthconnect"`, `"healthkit"`, and `"recorded"` for §4.0.4's in-app GPS recording) — Path 1 and Path 3 have their own endpoints and their own `source` values, so this list doesn't need to anticipate those. `name`/`description` are optional and empty for Health Connect/HealthKit sync, which never sends them; `activity_type`/`name`/`description` are all validated against the same `maxActivityTypeLen`/`maxActivityNameLen`/`maxActivityDescriptionLen` bounds `handleUpdateActivity` (§4.7.4) enforces for an edit after the fact — `activity_type`'s check matters specifically for §4.0.4's in-app recording, the one client that can send an arbitrary custom value here rather than a normalized one.

**No Path-2-specific branch in `ingest.Process`.** `internal/parse` gained a `.json` case (`ParseJSON`, dispatched by `ByExtension` the same way `.gpx`/`.tcx`/`.fit` already are) that decodes the wire shape above straight into the same `Activity`/`Point` structs the file parsers produce — there is no format-specific parsing left to do for Path 2 since the on-device app already read raw samples out of the platform health store itself, only a field-for-field reshape. Each activity's `{activity_type, points}` is re-marshaled and persisted to object storage exactly like a Path 3 raw file, then read back through the same `parseByExtensionReader` call `ingest.Process` already made — §4.0's "differ only in how bytes arrive, converge on §4.1 step 2" holds literally, not just in spirit.

**Idempotency uses the caller's own id, not a content hash.** Every other path (`persistAndEnqueue`, `internal/httpapi/server.go`) derives `external_id` from `sha256` of the raw bytes, because a plain file upload has no better-known identity. Path 2 activities already carry a stable id from the platform health store — a Health Connect session UUID, eventually a HealthKit workout UUID — which *is* the record's real identity; hashing the synced JSON instead would mint a new "activity" on every retry that happened to reserialize a field differently, defeating ROADMAP.md's resumable-retry requirement rather than serving it. `uploadFileParams` gained an optional `ExternalID` field for exactly this, and the object-storage key is scoped by source when it's set (`raw/{userID}/{source}/{externalID}{ext}`) rather than the flat content-addressed `raw/{userID}/{hash}{ext}` scheme — a caller-supplied id is only unique within its own source's namespace, unlike a content hash.

**Bounds**, the same "cap before reading the body fully" posture §5.1 states for Path 3: `maxSyncBatchActivities` (100 activities per request — a page of a foreground sync run, not a claim a real history is smaller), `maxSyncPointsPerActivity` (50,000, matching §5.2's own "100-mile ride at 1 Hz is 36,000+ points" ceiling for file formats), `maxSyncBodyBytes` (64 MiB). An activity needs at least 2 points to be accepted here — the same floor `ingest.Process` itself enforces post-privacy-trim — but this is a coarse pre-check, not a guarantee: an activity that clears it can still fail the deeper trim-based check asynchronously in the worker, exactly as already happens for Path 3.

**Verified against a live `db`/`minio`/`api`/`worker` stack**, not just unit tests: signed up a test user, posted a 3-point batch and confirmed `{"status":"enqueued"}`; confirmed the activity reached `state = 'done'` and appeared correctly in both `GET /v1/activities` and `GET /v1/uploads` (source-joined filename `hc-run-001.json`); read the row back from Postgres directly and confirmed `source = 'healthconnect'`, `external_id = 'hc-run-001'` (not a hash), and `raw_payload_key = 'raw/{userID}/healthconnect/hc-run-001.json'`; re-posted the identical batch entry and confirmed it returned `"already_processed"` rather than a second row; confirmed a batch of 101 activities is rejected in full with a 400, and confirmed the route requires an authenticated session.

#### 4.0.4 In-app GPS recording (Android)

**Built.** No new endpoint and no schema migration, per [ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md): a finished recording is normalized to the exact same wire shape §4.0.3 already accepts and posted to the same `POST /v1/sync/activities`, under `source = "recorded"` — the one server-side change was adding that value to `syncSources` (`internal/httpapi/sync_activities.go`). The client implementation — `apps/android/fitmap`'s `recording/` package, the deferred-sync design, and the account-scoping and demo-gating fixes it needed — is documented in `apps/android/docs/IMPLEMENTATION.md` §7, not here; this section covers only what the server needed to change to accept this path.

`external_id` is a UUID the app mints at recording start, not a platform record id — unlike Health Connect/HealthKit, there is no platform health store handing this activity an identity, so the client has to originate one itself. Idempotency (`(user_id, source, external_id)`) works exactly the same either way; a retried submit of the same recording is recognized rather than duplicated.

**This is the one ingest path that sets `name`/`description` at creation, not just through a later edit.** The recording screen's Name/Description fields ride along in the same batch entry `parse.JSONActivity` already carries (§3.3's `activities.name`/`description` columns), so the activity is titled from the moment `ingest.Process` first inserts it rather than needing a follow-up `PATCH /v1/activities/{id}` (§4.7.4) the way every other ingest path does. Health Connect/HealthKit sync leaves both fields empty, exactly as before — nothing about their payload changed.

**`activity_type` is free text on this path, not the fixed vocabulary Health Connect sync normalizes onto** (§4.7's `health/ExerciseTypes.kt` mapping doesn't apply here — there's no platform exercise-type field to normalize from). `syncOneActivity` validates its length against the same `maxActivityTypeLen` (50, matching the `VARCHAR(50)` column) `handleUpdateActivity` already enforces for an edit, added specifically because this path is the first one a client can hand an arbitrary string to — unvalidated, an over-length custom type would have failed as a raw column-width error deep in the worker instead of a clean `400`-shaped rejection here.

**Verified against a live `db`/`api`/`worker` stack**: posted a 3-point batch with `source: "recorded"`, a `name` and a `description`, confirmed `{"status":"enqueued"}`; read the row back from Postgres and confirmed `source = 'recorded'` and both `name`/`description` persisted exactly as sent, alongside the usual computed `distance_meters`; re-posted the identical entry and confirmed `"already_processed"`; confirmed an unrecognized `source` value still `400`s with the (now three-way) allowlist message. Client-side verification, including the deferred-sync flow, Delete, and the demo-gating and account-scoping fixes, is recorded in `apps/android/docs/IMPLEMENTATION.md` §7.5.

### 4.1 Ingestion pipeline

The order of these steps matters.

1. **Receive** — via any of the three paths above. Validate, check for an existing match on `(user_id, source, external_id)` as a fast path (§4.0's idempotency invariant), enqueue an `ingest` job, return. Nothing heavy happens inline. This check is an optimization, not the guarantee — see step 6.
2. **Parse** — stream `.FIT` binary or `.GPX`/`.TCX` XML, or map a provider's JSON. **Never load a whole file into memory**: a 100-mile ride at 1 Hz is 36,000+ points across a dozen channels, and a bulk archive is thousands of those.
3. **Apply privacy** — clip the raw point list against `privacy_zones` and trim `privacy_trim_m` from each end **before anything is persisted or indexed**. Privacy applied at render time leaks through any bug in the render path; privacy applied at ingest cannot.
4. **Compute coverage from the raw points** — rasterize each *line segment*, not each point, into the coverage mask (§4.2), and mark touched `fog_tiles` rows dirty. Upsert `user_tiles` for z14 and z17.
5. **Simplify for display only** — `ST_SimplifyPreserveTopology`, stored in `activities.trajectory`.
6. **Persist** — summary metrics to `activities`, sensor arrays to `activity_streams`, and the raw ingest payload to object storage (the uploaded file for Path 3, the provider's push for Path 1, the synced point batch for Path 2), keyed by `activities.raw_payload_key`. This, not `activity_streams`, is what makes retroactive re-clipping possible (§7) — it is the only place full raw points survive past this job, since `activity_streams` stores elapsed time and sensor channels but not position. The `activities` insert is `ON CONFLICT (user_id, source, external_id) DO NOTHING`, matching `idx_activities_dedupe` (§3.3) — this, not step 1's check, is what actually enforces §4.0's idempotency invariant under concurrent duplicates.
7. **Re-render dirty fog tiles** — as a follow-up job (§4.2).

**Step 3's trim is point-density-independent, not just distance-independent.** `internal/ingest/trim.go`'s `TrimEndpoints` walks forward from the start and backward from the end, but interpolates a synthetic point at exactly `trimM` along whichever segment crosses that distance, rather than snapping to the nearest existing point — so the trimmed result depends only on `trimM` and the track's real geometry, never on how sparse the original recording was. The short-track fallback (keep the track whole rather than emit an empty one) checks the track's actual total walked length against `2 × trimM`, not where the two index-based walks happen to land — an earlier version checked the latter, which degraded to a silent no-op for a real, reachable class of input: a short track with only a handful of widely-spaced points could have a single segment overshoot `trimM` by enough that the two walks crossed before either reached a real cut point, returning the track completely untrimmed, endpoints included. Found via the Demo Customer account's real data (`Car Ride 2026-09-06`, 8 points over ~890m, came back 4.4m from home instead of ~200m) — not a demo-only artifact, since a real account's short, sparse upload (an errand drive with few recorded points) can trigger the identical case. Covered by `internal/ingest/trim_test.go`, including a regression test built from that activity's real coordinates; a full re-seed of the Demo Customer account's 611-activity history afterward found zero remaining activities starting within 50m of home on anything over 400m long.

### 4.2 Fog of War — precomputed raster mask

**Storage.** Coverage is a **bitmap**, not a set of cells — a per-user, per-viewport `ST_Union()` of thousands of polygons on every tile request would be a hard scaling wall a bitmap avoids entirely. For each z14 tile the user has touched, render a 512×512 single-channel PNG. At z14 (~2,446 m across) a 512 px tile is **~4.8 m/px** — finer than GPS accuracy. Resolution becomes a rendering parameter rather than a storage explosion.

**Rendering a tile.** Draw every trajectory clipped to the tile envelope, stroked at the reveal radius (**~20–40 m** in map units), into the alpha channel. Blur slightly so reveal edges are soft rather than serrated — but only slightly: references feather over roughly 1–4 px at display resolution, a hairline, not a haze. At ~4.8 m/px that is a blur radius on the order of 10–20 m.

Rendering a tile needs the *raw* points of every activity crossing it — `activities.trajectory` is the simplified display geometry (`docs/ARCHITECTURE.md` §1.1's "index raw points; simplify only for display" decision), and `activity_streams` has sensor channels, no position. §4.2.3 covers how this is actually served: a mask rendered per activity per tile at ingest time, so rendering cost stays proportional to just the activity that changed, not a per-request re-parse of every raw payload touching the tile.

The radius was reduced from 40–60 m after measuring references (§4.2.1): a tight reveal keeps individual streets separable in dense grids, where a wide one merges parallel streets into a blob. It also makes clearing an area meaningfully harder, which is the game.

**Pyramid.** Generate z13 down to z0 by successive 2×2 downsampling from z14. Above z14 the client overzooms the z14 raster, which is exactly right — the mask is a soft alpha channel, so bilinear magnification looks better than hexagon edges.

**Invalidation.** Ingest marks touched tiles `dirty`; a worker re-renders them and walks the pyramid upward. A new activity dirties a handful of z14 tiles, so incremental cost is near-constant regardless of history size.

**Volume.** A user covering a whole city touches a few hundred z14 tiles; a well-travelled user, a few thousand. Single-channel PNGs of mostly-empty coverage compress to a few KB. Total per-user storage is measured in megabytes.

**Serving.** The stored mask is a coverage alpha channel, but MapLibre cannot invert or colourise it at render time, so the tile route emits a **ready-to-draw RGBA PNG**: fog colour in RGB, `alpha = fog_opacity × (255 - coverage)`. Theme is part of the URL so the CDN caches one variant per theme, and storage holds only the single-channel master.

**The fog is a dark veil now, not a white one.** §4.2.1's reference measurements originally settled on white (`fog_colour = #FFFFFF`, `fog_opacity ≈ 0.66`), and that shipped first — but found live against this app's own light cream basemap, white-on-cream is two similarly light colours, so unexplored ground barely read as covered at all ("hardly distinguishable," reported directly). `fog_colour` is now the app's own dark ink token (`--fm-ink` in index.css, `#202B25`) at `fog_opacity = 0.82`: a dark veil reads as real, unambiguous contrast against a light basemap regardless of the basemap's own exact shade, which white never guaranteed. Unexplored ground now reads as dark and hazy; explored ground looks normal — the same "washed vs. normal" contrast §4.2.1 always wanted, just inverted in lightness to actually deliver it against this basemap.

```http
GET /tiles/v1/fog/{z}/{x}/{y}.png?theme=dark
```

Authorization follows §7 — the user is derived from the session, never from a parameter.

**Client compositing**, identical on MapLibre GL JS and MapLibre Native — this is Fog mode specifically; §4.2.2 covers how Heatmap mode composites differently:
1. Base layer — self-hosted Protomaps basemap. Choose a **rich, saturated flavor**: a washed-out base leaves the least to reveal once fog covers the rest — this reasoning predates the fog-colour change above and holds regardless of which colour the veil itself is.
2. Fog layer — the fog RGBA tiles as a plain `raster` source, inserted **beneath the basemap's first symbol layer** so place labels stay legible on top of the fog.
3. Track layer — vector tracks from `/tiles/v1/tracks/{z}/{x}/{y}.mvt`, over cleared regions.

> **Why the inversion is server-side.** The original spec said "a full-viewport dark rectangle whose alpha is driven by the *inverse* of the coverage raster". MapLibre cannot express that. Verified against `@maplibre/maplibre-gl-style-spec` 26.4.2 (2026-09-08): `paint_raster` offers only `raster-opacity`, `raster-hue-rotate`, `raster-brightness-min`/`-max`, `raster-saturation`, `raster-contrast`, `raster-resampling` and `raster-fade-duration`. There is no `raster-color` / `raster-color-mix` — Mapbox GL has it, MapLibre issue #4479 is open. Baking the inversion server-side keeps every client on a stock `raster` layer, and avoids writing the shader twice against two bindings.

#### 4.2.1 Reference implementations — measured, and where we deviate

Three Fog of World screenshots (`reference/fog-of-war-example*.{webp,jpg}`) were sampled to pin these numbers down rather than guess them.

| Property | Measured | Decision |
| :--- | :--- | :--- |
| Fog colour | White. Fogged ≈ RGB (193, 192, 188); revealed ≈ (63, 66, 58) | Adopted white first, then reverted to a dark veil (`#202B25`) once shipped against this app's own light basemap made a white veil hardly distinguishable — see the current value above, not this table |
| Fog opacity | ~0.66, consistent to ±0.02 across all three | Adopted `0.66` first; now `0.82` alongside the colour change above |
| Desaturation | Saturation falls 0.15 → 0.03 under fog | **Not a separate effect.** A 66% white blend produces exactly this arithmetically |
| Edge softness | 1–4 px transition at display resolution | Adopt — a hairline feather |
| Reveal radius | Hugs the road centreline, ~2–3 lane widths | Adopt — ~20–40 m |
| Label handling | Labels drawn **over** the fog | Adopt — fog below the first symbol layer |

**Where we deliberately deviate: no satellite imagery.** All three references composite fog over *aerial* imagery, and that is where their impact comes from. Protomaps is vector-only, and metered imagery is an unbounded per-request bill a free product cannot carry. **Decision: vector only.** Pick a flavor with strong landuse, water and road-casing colour, and treat the fogged state as "the same map, drained of colour" rather than "the map, hidden".

**These references set the mechanic, not the visual bar.** Matching the table reaches parity on behaviour; the differentiation comes from render quality on top of it.

#### 4.2.2 Heatmap mode

The third of the three modes `VISION.md` §4.2's Core Features table names (Fog of War, heatmap, and track/normal modes). Reuses fog's raster pipeline rather than building a second one — the underlying question is the same shape, "how much of this tile's ground has this user's history touched," just answered two different ways.

**Fog answers a binary question per pixel; heatmap answers a graded one, and that difference is why heatmap can't just recolour the fog mask.** Fog's stroke pass sets a pixel to "revealed" — effectively a max, not a sum — precisely so a well-travelled street and a once-ridden one look identical (§4.2.1: the reveal mechanic depends on that). Heatmap needs the opposite: additive accumulation, one pass per trajectory segment through a pixel, so a daily commute glows brighter than a road ridden once — the entire point of a heatmap.

**Storage: a second raster per tile, accumulated additively, not a second pipeline.** Same z14 tile grid as fog, same ~4.8 m/px resolution, same segment-rasterization at ingest (§4.1 step 4) — but each stroke adds to a running intensity value per pixel instead of setting a flag. That needs more headroom than fog's effectively-1-bit-per-pixel semantics: a wider accumulator internally, log- or sqrt-scaled down to the 8-bit single-channel PNG actually stored, the same compression Strava-style heatmaps need so a handful of very-hot pixels (a driveway everyone crosses, a commute ridden daily) don't blow out the visible range for everything else.

`fog_tiles` (§3.6) gains a second object key for this — `heatmap_object_key` alongside the existing one, sharing the same `(user_id, zoom, tile_x, tile_y)` row, the same `dirty` flag, and the same re-render job, since one ingest event dirties both rasters for the same tile at the same time. Not a separate `heatmap_tiles` table: that would duplicate the entire dirty-tracking scheme for data invalidated by exactly the same events. **Unlike `object_key`, `heatmap_object_key` is not an all-time aggregate** — §4.2.3 covers the rolling window it's actually built from, and why that has to be re-derived on a schedule, not just on ingest events the way Fog's own aggregate is.

**Serving: a baked colour ramp, for the same reason fog's inversion is baked server-side.** MapLibre still has no `raster-color`/`raster-color-mix` (§4.2's own verified note against `@maplibre/maplibre-gl-style-spec` 26.4.2 — MapLibre issue #4479 is still open), so a client-side gradient from a single grayscale channel isn't an option here either. The heat ramp — transparent at zero, through a couple of hue stops, to a saturated hot colour at the high end — is baked into the served RGBA PNG server-side, exactly like fog bakes its own dark-veil inversion. Both are a plain precomputed-aggregate lookup at request time (§4.2.3) — the ramp is the only thing this step does at all:

```http
GET /tiles/v1/heatmap/{z}/{x}/{y}.png
```

Same authorization rule as fog and tracks: the user comes from the session, never a parameter (§7).

**Client compositing — one more mode, not a fourth layer stacked on the other two.** The three modes are mutually exclusive views of the same underlying history, switched by a single on-map toggle, not independent checkboxes:

- **Normal** — base layer + track vector layer only. This is what already ships today; it needed no new work to exist as a "mode."
- **Fog** — base layer + fog raster (§4.2's dark veil), track layer hidden.
- **Heatmap** — base layer + heatmap raster in place of the fog layer, track layer hidden.

Track lines are redundant over either raster — the raster already encodes where (and, for heatmap, how much) — and drawing both reads as visually noisy rather than additive, undercutting the fog/heatmap effect rather than complementing it. Both modes hide the track layer.

**Privacy is unaffected, not just inherited.** Both rasters are computed from the same post-trim, post-privacy-zone point stream (§4.1 step 3 runs before step 4, for both) — heatmap doesn't see anything fog doesn't. The retroactive-reprivacy job (§7) that re-renders a fog tile when a new privacy zone is added has to re-render its heatmap tile alongside it, for the same reason `heatmap_object_key` lives on fog_tiles' row rather than a separate table: the two go dirty together, from the same events, every time.

**Built, with concrete adopted values, recorded the same way §4.2.1 recorded fog's.** The accumulator isn't "all tracks share one canvas" (that would just reproduce fog's max, per the reasoning above) — each track renders to its own temporary stroke mask (same stroke radius and feather as fog, so the two mechanics agree on "how wide is a route") and the masks sum into a shared float accumulator, pixel by pixel. The sum is normalized against **`users.heatmap_cap`**, a per-account value (`migrations/0019_heatmap_cap.sql`, default 8.0 for a fresh account), then compressed with **sqrt**, not log, so a single pass already reads as visibly warm rather than needing to approach the cap to show up. Colour ramp, linearly interpolated between stops: transparent at zero → RGB(178, 24, 24) at 15% (alpha 130) → RGB(255, 140, 0) at 55% (alpha 210) → RGB(255, 235, 60) at 100% (alpha 255).

**`heatmap_cap` is adaptive, not the fixed constant it started as.** `internal/fog.RecomputeHeatmapCap` sets it to the account's own single most-touched z14 tile: how many currently-in-window, non-superseded activities cross it (`COUNT(DISTINCT activity_id)`, grouped by tile, over `activity_tile_masks`). A percentile statistic was tried first and rejected, live against the Demo Customer account: of its 1,140 distinct touched z14 tiles, 1,100 are touched exactly once, and the two genuinely hot tiles (591 and 498 touches) are under 0.2% of that population — any percentile at or below the 99.7th still lands inside the once-touched mass, nowhere near reachable at the 90th-95th range a less extreme account might tolerate. The max targets the actual problem directly instead: the account's single most-used spot is the one place that should read as fully hot only at its true busiest, with everything else scaled down relative to it. It's also hard to spoof — the count is per-*activity*, not per-point, so one activity idling or jittering in place can't inflate it, only genuinely many separate activities crossing the same tile can.

Two guards keep this from misfiring: below `minTouchedTilesForAdaptiveCap` (20) distinct touched tiles, an account hasn't built up enough history for its own max to mean anything yet, so the recompute leaves the current cap alone rather than let two early activities sharing one tile set it to 2. And because the cap is baked into the stored heatmap PNG (not applied at request time), changing it means re-rendering every one of the account's tiles — `heatmapCapChangeThreshold` (15%) skips the update, and the re-render, when the new value wouldn't move the picture enough to justify it.

`internal/worker`'s `recomputeHeatmapCaps` runs this daily for every user with any in-window activity — the same ticker cadence as `ageOutHeatmapWindow`, run independently: whichever of the two happens to run first on a given day, the other self-corrects on its own next tick from whatever `in_heatmap_window` state exists by then, so there's no ordering dependency between them. When a user's cap actually changes, the sweep updates `users.heatmap_cap`, marks every one of that user's `fog_tiles` rows dirty (every touched tile already has one, from the first ingest that touched it), and enqueues the same `render_fog` job ingest already uses — Fog's own tiles get needlessly rebuilt alongside Heatmap's in that pass, since one `dirty` flag covers both rasters together, but that's the existing shared-dirty-flag cost every other dirty-marking path already pays, not something new here.

Verified directly, not assumed: a pixel crossed by one activity, then a second activity through the exact same pixel, measurably brightened and gained opacity on re-render (a real RGBA delta, decoded from the actual served PNG) rather than staying at a single on/off state — and the same pixel in fog's own tile was unaffected by the second pass, confirming the two rasters are computed independently from the same input rather than one leaking into the other.

#### 4.2.3 Per-activity masks: the ingest-time building block behind both rasters

Fog and Heatmap ignore the Activities panel entirely — no date range, Type/Distance filter, or hidden-track set narrows either one; both are switched to from the map-mode toggle, which also hides the panel and date picker while either is active (`apps/web/src/map/MapView.tsx`'s `changeMapMode`). Fog shows the account's true all-time coverage; Heatmap shows a fixed rolling window ending now (`fog.HeatmapWindowDays`, currently 365 days, not user-configurable) so a route no longer visited can cool off instead of staying maximally hot forever — see §4.2.2's own note on why heatmap needs a window fog deliberately doesn't.

Both still rely on the same **crisp (unblurred) mask per activity, per z14 tile it touches** (`activity_tile_masks` — `services/server/migrations/0005_activity_tile_masks.sql`), rendered once at ingest from the points already in memory (`internal/fog.RenderActivityMasks`, called from `ingest.Process` right after persisting the activity) — no raw-payload re-fetch, and ingest-time rendering cost is proportional to just that one activity's own tiles, independent of how many *other* activities already touch them. This is what makes deleting or superseding an activity cheap and correct: `renderAndStoreTile` always fully recomposites a dirty tile from whichever `activity_tile_masks` rows still reference non-superseded activities, never an incremental subtraction, so a delete just means one fewer row feeding that composite rather than a raw-payload re-fetch and re-parse of every other activity sharing the tile (payloads expire on a retention schedule — that path isn't always even possible).

**Both `GET /tiles/v1/fog/{z}/{x}/{y}.png` and `GET /tiles/v1/heatmap/{z}/{x}/{y}.png` are now a plain lookup** — `object_key`/`heatmap_object_key` respectively, off the same `fog_tiles` row, with no per-request compositing of any kind. They differ only in what `renderAndStoreTile` fed into each column: Fog's composite always includes every non-superseded activity's mask; Heatmap's only includes masks whose activity's `started_at` falls inside the rolling window (`fog.HeatmapWindowDays`) *as of that render*.

**This wasn't always a lookup for Heatmap — it used to composite the window live, on every request** (`internal/fog/filtered.go`'s `RenderFilteredTile`, since removed). Measured against the Demo Customer account's home tile (591 of 611 activities): ~12.6 seconds per tile, ~56.5 seconds at low zoom. The 45-second in-process cache meant to absorb repeated requests within one pan/zoom gesture never actually helped, either — its key embedded the filter's exact `From` instant, and a fresh `time.Now().AddDate(0, 0, -365)` call on every request almost never reproduces the same instant twice, so it missed on effectively every request. Reusing Fog's own precomputed-lookup shape fixes both problems at once, at the cost of the rolling window only moving on a schedule rather than continuously — see below.

**Keeping the window current without a live per-request check**: an activity entering the window (a fresh upload) is handled by the exact same ingest-time dirty-marking Fog already has — nothing new there. An activity *leaving* the window is a pure function of time passing, which ingest/delete events have no reason to ever notice, so window membership is a stored fact rather than something recomputed against "now" at render time: `activities.in_heatmap_window` (`migrations/0018_activity_in_heatmap_window.sql`, default `true` — a fresh activity is by definition within the window), read directly by the query above. `internal/worker`'s `ageOutHeatmapWindow` runs daily (`heatmapAgingInterval`), finds whichever activities have just crossed the boundary (`in_heatmap_window AND started_at < now - 365d`), flips each one's flag, and re-renders only *that activity's own* touched tiles (`ingest.ActivityTiles`, the same per-activity lookup `handleDeleteActivity` uses) — never a whole-account sweep. This is deliberately per-activity and daily rather than per-account and weekly: activities age out on whatever calendar day happens to be 365 days after they were originally created, which are already scattered across the year, so the daily sweep's workload is naturally small and spread out on its own, with no artificial staggering needed. The trade-off is explicit: an aged-out activity takes up to a day to cool off, not exactly 365 days to the hour — accepted, since nothing about this feature is watched closely enough in real time for the difference to matter.

**A known gap, worth naming for whenever it's built**: there is no reprivacy job yet (§7's own retroactive-reprivacy re-render has no implementation, and there's no way to change `users.privacy_trim_m` today either). When one is built, it needs to re-call `RenderActivityMasks` with freshly re-trimmed points per affected activity — masks are composited from what's already rendered, not re-derived from raw points on each read, so nothing notices a trim setting changed underneath them on its own.

#### 4.2.4 Country/Region zoom tiers

Below a threshold zoom, Fog and Heatmap's per-pixel raster (§4.2, §4.2.2) is replaced entirely by a coarser whole-polygon reveal: a country or state/region renders as fully "unlocked" the moment the account has a single activity anywhere inside it, however small or brief. The two tiers are switched purely by MapLibre `minzoom`/`maxzoom` on the layers themselves (`apps/web/src/map/zoomTiers.ts`; `apps/android/...MapOverlays.kt` mirrors the same four constants) — Country at z0–z4, Region at z5–z7, City (the untouched raster pyramid) at z8 and above — never layered together and never reconciled against each other: at country/region zoom there's no fine-grained detail visible to contradict, so the coarse reveal already is the whole truth at that scale.

**Boundary data**: `admin_countries`/`admin_regions` (§3.12–§3.13) hold Natural Earth's country and state/province polygons, loaded once by the `seed-admin-boundaries` subcommand (`cmd/fitmap`) — an idempotent, re-runnable load mirroring `seed-demo-customer`'s own shape, embedded via `go:embed` from `internal/geo/seed-data/*.geojson` rather than fetched over the network at deploy time. The vendored files are pre-simplified (Douglas-Peucker, ~800m tolerance) and coordinate-rounded from Natural Earth's raw shapefiles, since the raw 1:10m region export is ~52 MB of full-precision vertices for a layer that only ever renders below z8 — simplified, it's ~9 MB.

**Matching**: `internal/geo.MatchActivity` runs once per activity, called from `ingest.Process` right after the row is persisted, and inserts into `activity_country`/`activity_region` (§3.14–§3.15) for every polygon the activity's trajectory intersects. `seed-admin-boundaries` also backfills this for every activity that predates the feature — not optional: without it, every existing account (including the Demo Customer) would show every country locked until its next upload.

**Serving**: live vector tiles (MVT), not a precomputed raster — see ADR-0008 for the reasoning. Four endpoints, mirroring `handleTracksTile`'s live-query shape:

```http
GET /tiles/v1/country-fog/{z}/{x}/{y}.mvt
GET /tiles/v1/country-heatmap/{z}/{x}/{y}.mvt
GET /tiles/v1/region-fog/{z}/{x}/{y}.mvt
GET /tiles/v1/region-heatmap/{z}/{x}/{y}.mvt
```

Fog's query returns the **locked** set — countries/regions with no matching `activity_country`/`activity_region` row for the current user (`superseded_by IS NULL`) — rendered as a flat fill in fog's own veil colour (`#202b25` @ 0.82, identical to `RenderFogPNG`'s `fogColour`/`fogOpacity`, so nothing shifts hue crossing the zoom boundary); unlocked polygons are simply absent from the tile, same "no veil = revealed" semantic as the raster tier. Heatmap's query returns the **unlocked** set — with a matching row *and* `in_heatmap_window` true, so a country visited only long ago cools off at this tier exactly as it already does at city zoom — rendered as a flat fill in the heatmap ramp's own base hue (`#b07e2e`, also `tracks.ts`'s `TRACK_COLOR`) at a fixed moderate opacity (0.45): "you've been here," not graded by how much — a whole-country intensity gradient would be a second scoring dimension nobody asked for.

**Why a live query is safe here despite the cost that ruled it out for Heatmap's own raster tier (§4.2.3)**: `admin_countries`/`admin_regions` have a small, fixed row count (~250 / ~4,600) that never grows with a user's activity history; each query is one indexed `EXISTS`/`NOT EXISTS` join against `activity_country`/`activity_region`, never the `ST_Intersects` geometry test itself, which only ever runs once, at ingest.

**Deletion**: `ON DELETE CASCADE` on `activity_country`/`activity_region` means `handleDeleteActivity` needs no new code — a deleted activity's membership rows disappear with it, and the next tile request simply sees a different join result. There is no dirty flag, cache, or rebuild step for this tier to invalidate.

### 4.3 Track vector tiles

Tracks — unlike fog — remain a live query, because they must respond to filters that cannot be precomputed.

```http
GET /tiles/v1/tracks/{z}/{x}/{y}.mvt?from=2025-01-01&to=2025-12-31&types=run,ride
```

```sql
SELECT ST_AsMVT(t, 'tracks', 4096, 'geom')
FROM (
    SELECT id,
           activity_type,
           ST_AsMVTGeom(
               ST_Transform(ST_Force2D(trajectory), 3857),
               ST_TileEnvelope($1, $2, $3),
               4096, 64, true
           ) AS geom
    FROM activities
    WHERE user_id = $4
      AND trajectory && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)
      AND started_at BETWEEN $5 AND $6
      AND activity_type = ANY($7)
) t;
```

Apply zoom-dependent simplification inside `ST_AsMVTGeom` and keep tiles under ~50 KB. Cache on the CDN keyed by the full query string; filter combinations are few in practice.

`handleTracksTile` runs `parseActivityFilter` and applies it server-side. `apps/web/src/map/tracks.ts`'s `trackTileURL()` takes the current range, and `MapView.tsx` calls `refreshTrackLayer` whenever `selectedRange` changes — `ensureTrackLayer` alone can't do this, since its `addSource` call only runs once and is a no-op once the source exists. An absent filter means "no restriction" by convention (§4.0), so the tracks layer must always send `from`/`to` explicitly to stay in sync with the Activities list beside it. `types` stays client-side only (`activityFacets.ts`/`setHiddenTracks`), matching DISTANCE, which this endpoint has no equivalent parameter for at all.

#### 4.3.1 Colored zone segments (speed / heart rate)

A second, small GeoJSON layer (`apps/web/src/map/trackBands.ts`), drawn over the shared MVT tracks layer above for whichever single activity is currently selected (`selectedActivities.size === 1` in `MapView.tsx`) — not a change to that layer, since a vector-tile source can't carry the per-vertex color this needs. `GET /v1/activities/track-metrics/{id}` (`internal/httpapi/activities.go`) computes it at read time, no schema change: `trajectory`'s own vertices (`ST_DumpPoints`, the same shape `simplifyXY` produces at ingest) give position and a timestamp (the M ordinate) per simplified vertex; `activity_streams.elapsed_s`/`heartrate` (raw, full-resolution) give the rest. Speed is derived directly from consecutive simplified vertices (`ingest.HaversineM`/`Δt`); heart rate is matched to the nearest raw point by elapsed time (`sort.Search` — a nearest-neighbor search, unlike `matchSimplifiedTimes`' exact-coordinate one, since simplified vertices don't land on exact raw timestamps the way they land on exact raw coordinates). Heart rate is all-or-nothing per activity: any gap in coverage and `heartrate_available` is `false` for the whole response, rather than a curve that understates effort by silently skipping missing points.

The frontend bins each activity's own min/max (not a fixed scale — "where in this run was I fastest" is a question about that run) into 5 bands and renders one merged `LineString` feature per contiguous run of same-band vertex pairs, colored via a plain `'line-color': ['get', 'color']` — no `line-gradient`/`lineMetrics` needed, since each feature already carries its own resolved color. The Pace/Heart rate toggle (`MapView.tsx`) only renders when `heartrateAvailable` is `true` — with no heart-rate data there is nothing to switch to, so a single-option toggle is simply omitted rather than shown disabled; the colored strip/curve themselves always render (pace only, in that case). There is no separate color-key legend — exact band values are read from the hover tooltip in the pace/heart-rate + elevation profile (§4.3.2) instead, which already needs that same lookup for its own tooltip.

Row-click focus (`focusedActivityId`) is what this layer, and §4.3.2's panel, key off of; it is cleared not only by focusing a different activity but also by **clicking away** — one plain (non-layer-scoped) `map.on('click', ...)` handler in `tracks.ts` hit-tests the click and calls either `onSelect(id)` or `onClickAway` (wired to `setFocusedActivityId(null)` in `MapView.tsx`) depending on the result — a single query decides both outcomes, rather than a layer-scoped `onSelect` listener and a separate plain `onClickAway` listener each running their own, which could only ever agree by construction anyway (see the tolerance note below for why they were merged for real).

The hit-test queries a **small box around the click** (`map.queryRenderedFeatures([[x - 4, y
- 4], [x + 4, y + 4]], { layers: [TRACKS_LAYER_ID] })`, `CLICK_TOLERANCE_PX = 4`), not the bare pixel — with `NORMAL_WIDTH` at 2.5px, an exact-pixel query leaves almost no margin for error: a click a few pixels off a second activity's thin line, while a first one is already focused, would miss the second activity entirely and clear the first activity's focus instead of leaving it alone. The box is large enough that a few-pixel miss still lands inside it, small enough not to casually pick up an unrelated, more-distant track — a tolerance problem, not a race between two competing hit-tests.

**Known limitation, not yet worked around**: because this reads the *already-simplified* `trajectory` rather than the raw points, vertex density follows Douglas-Peucker's geometric-deviation rule, not speed/HR variation — a segment with little directional change (a straight road, a short slow segment that doesn't cover much distance) can be simplified down to very few or even zero surviving vertices, regardless of how much its speed or heart rate actually changed there. Confirmed directly: a synthetic two-segment activity (slow, then fast, both on a slightly winding path) simplified down to 8 vertices, *all* from the fast segment — the entire slow segment's own speed was invisible in the resulting bands. A more winding real-world route (three segments, more heading variation) kept 16 vertices spanning all three speeds correctly. The mechanism itself — banding, merging, coloring, the toggle, the legend — is confirmed correct in both cases; it's specifically the *source resolution* that can be too coarse on a geometrically simple track. **Recomputing colors against raw points alone would not fix this** — a band segment can only be as fine-grained as the *line's own vertices*, so re-matching the same sparse simplified vertices against better data still produces the same sparse bands. Fixing it properly means re-parsing the raw payload from object storage on request (the same "derived data re-parses rather than reads `activity_streams` back out" precedent §4.5's closing note already establishes for curves) and drawing the band segments along *that* full-resolution polyline instead of `trajectory` — replacing the geometry this layer renders, not just the color matched to it. For the one focused activity, that denser overlay would still fully cover the plain simplified track underneath, same as today; every other activity stays exactly as simplified as it already is. Not done here — a typical activity is a few thousand points at 1 Hz, cheap enough to re-parse for one on-demand focus click, but it's still a real added cost per click for a limitation that doesn't show up on every activity, only geometrically simple ones.

#### 4.3.2 Pace/heart-rate + elevation profile

A second, complementary view of the same per-vertex data §4.3.1 already fetches — not "where on the map was I fast/slow" but "how did pace/heart rate track against elevation over the route" — as a straight-line strip rather than following the geographic track, so climb and effort read side by side. Mounted inside the same floating card as §4.3.1's Pace/Heart rate toggle (deliberately, per direct user preference: "I like how it is now... this is why existing panel look good to me" — a small, compact overlook, not a new docked panel or a bigger, more detailed instrument), so it shares that toggle's state and appears/disappears on exactly the same condition (row-click focus, `MapView.tsx`'s `focusedActivityId`) with no new visibility logic of its own — including clearing when focus is cleared by clicking away from every track (§4.3.1).

This panel's hover tooltip is the only place exact band values are shown at all — there is no separate legend (removed per direct user feedback: "we don't need legend (hover is enough)"). The toggle above it is §4.3.1's own toggle, so it inherits that same `heartrateAvailable`-gated visibility; this panel's strip and elevation curve render regardless of whether the toggle itself is shown, using whichever metric is currently selected (pace, if heart rate isn't available).

`GET /v1/activities/track-metrics/{id}` (§4.3.1's own endpoint) gained two things to support this: a **cumulative distance** per vertex (`distance_m`, always present — computed in the same loop that already calls `ingest.HaversineM` for speed, just also accumulated) and **elevation** per vertex (`elevation_m`, gated `elevation_available` the same all-or-nothing way `heartrate_available` already is, matched to the nearest raw point via the same `nearestElapsedIndex` lookup heart rate already uses). Distance, not vertex index, is what the x-axis is laid out by — vertex density follows Douglas-Peucker's geometric simplification (§4.3.1's own caveat), so an index-based x-axis would visually compress or stretch sections that have nothing to do with real distance, exactly the correlation this view exists to show.

The colored strip reuses §4.3.1's own band computation exactly: `trackBands.ts`'s band-merging loop was extracted into an exported `computeBandRuns` (points/metric/scale → contiguous same-band index runs) so both consumers — the map's curved `LineString` segments and this straight strip's distance-proportional `<div>` widths — share one algorithm rather than two copies of it. The elevation curve beneath it is a small inline SVG, scaled to *this activity's own* min/max elevation with no axis labels or gridlines — "the user doesn't need too much detail, just a general overlook," per the user directly, not a design assumption. Hovering either the strip or the chart (one shared handler, since both share the same x-axis) finds the nearest vertex by distance fraction and shows a small fixed tooltip — the same hover-tooltip shape `Trends.tsx` already uses, not a new interaction pattern.

#### 4.3.3 High-resolution map export

`VISION.md` §4.2's "Export" pillar, first slice — see §5.5 for why this is client-side rather than a server-side render. `apps/web/src/map/exportMap.ts`'s `exportMapImage` builds a second, temporary, hidden `MapLibreMap` in an off-screen container (`position: fixed; left: -99999px` — not `display: none`, which leaves an element with no real layout box for WebGL to size a canvas against) sized to a fixed target width and the *live* map's current aspect ratio, constructed with `canvasContextAttributes: { preserveDrawingBuffer: true }` (nested there, not a top-level `MapOptions` field, in the installed maplibre-gl 6.9.0 — confirmed against its own type declarations). The live map deliberately never sets this itself, confining its real, if usually small, perf cost to the temporary instance that only exists while exporting.

Once `'load'` fires, the hidden instance gets the exact same calls `MapView.tsx`'s `reattachOverlays` already makes against the live one — `ensureFogLayer`/`ensureHeatmapLayer`/`ensureTrackLayer`, `setMapMode`, `setHiddenTracks` — so Normal/Fog/Heatmap and the current date-range/hidden-track filters all export faithfully with no new rendering logic. Waits for `'idle'` plus a 150 ms settle buffer (the same margin `docs/DEVELOPMENT.md`'s `isStyleLoaded()` gotcha already established for "a just-finished render isn't always actually finished") before reading `canvas.toBlob('image/png')`, then tears the instance and container down. `ExportButton.tsx` triggers the browser download via a blob URL and a synthetic `<a download>` click — no server round trip, so no cost to rate-limit.

**Not in this slice**: colored zone segments (§4.3.1 — a single-focused-activity view, narrower than "export my map"), vector/SVG output (raster only), and story cards/animated reveals (`VISION.md` §4.2's other two "Export" items, unbuilt).

### 4.4 Explorer tile scoring

`user_tiles` is small — thousands of rows per user, not millions — so scoring is a direct query rather than a pipeline: total tiles, max square, max connected cluster, per-region coverage percentage. Deliberately separate from the visual fog so neither constrains the other's resolution.

### 4.5 Per-activity pace/heart-rate, and distance trends

Not a performance-analysis pillar — `VISION.md` §1.1 draws a hard line against FitMap being a health or fitness advisor. What's here is deliberately narrow: pace and heart rate shown as route context on a single activity (computed from `activity_streams`), and a plain distance/time rollup over a period. No HR zones, no power, no training load, no all-time performance records.

**Ingest-gap fixes, built to unblock this.** Scoping this section against the real ingest code found two gaps: `moving_seconds` was declared in §3.3 but never populated (no stopped-time detection existed), and no per-point cumulative distance was stored anywhere. `internal/ingest`'s `computeMetrics` now computes `movingS` (a point-to-point speed threshold, ~1.8 km/h, excludes a segment below it from moving time) and a per-point cumulative-distance array (`dist_m`, §3.4) in the same pass that already walked every point for `distanceM` — no second scan. None of this is user-visible on its own; it exists so Trends and the per-activity pace/HR profile (§4.3.2/FR-4.9) don't each need their own ingest change.

**Trends** — count/distance/moving-time/elevation-gain per week or month, `GET /v1/activities/trends?bucket=week|month` — is this section's own UI (`apps/web/src/ui/Trends.tsx`, on `ProfilePage.tsx` below the activity grid): it needed no schema change and reuses §4.8's stat-query shape.

**Built and then cut.** Best-effort curves and personal bests (`GET /v1/activities/best-efforts`, `GET /v1/activities/personal-bests`) shipped, along with `cadence`/`power_w` capture (§3.4), grade-adjusted pace, HR zones, time-in-zone, power curves, a training-load concept, and Oura recovery data (sleep/HRV/readiness) as a planned future join — all squarely in the "health or fitness advisor" territory `VISION.md` §1.1 now rules out. The two derived-stat tables were dropped (`migrations/0012_drop_best_efforts_and_splits.sql`, `0013_drop_cadence_power.sql`); the rest never got past being documented here. None of it is coming back under the current vision.

### 4.6 Cross-source deduplication

Unavoidable the moment a second path exists, and every multi-device athlete hits it immediately: a ride recorded on a Garmin syncs via Path 1, gets written to HealthKit and syncs via Path 2, and appears again in a Strava bulk export via Path 3. Three copies, three different `external_id`s, one real ride. The unique index in §3.3 does not catch this — the identities are genuinely different.

**Built**, in `internal/ingest/dedupe.go`, run from `ingest.Process` once the new activity's streams and masks exist — richness is read off those rows, so ranking a record ahead of its own streams would judge it the poorest copy every time.

Identity is fuzzy: **same user, same activity type, start within half a minute either way, distance within ~1%.** It is applied as a window around the incoming activity rather than as equality on a pre-rounded bucket. Rounding first is cheaper to index but turns matching into a lottery at the bucket edges — two copies three seconds apart match or don't depending purely on whether they straddle a boundary, which was measured happening on a real pair. A window applies the same tolerance to every pair the same way. `idx_activities_dedupe_window` (§3.3) is what keeps the range scan cheap. Tolerance is relative rather than absolute because 1% of a 5 km run and 1% of a 200 km ride are very different numbers of metres.

Collisions prefer the **richest** record — geometry over none, then more stream channels over fewer — and the rest are marked `superseded_by` rather than deleted, so a user can see why something disappeared (`GET /v1/activities/duplicates`, §4.7). The remaining comparisons are a tie-break rather than a richness judgement, and they have to be total and stable: two identical-looking copies must resolve the same way whichever was examined first, or a re-run flips which one is live and churns every fog tile underneath it.

Start times differ by a few seconds between sources, which is why the tolerance exists at all, and why this is a heuristic rather than a key. Get it wrong in the permissive direction and two real activities merge; wrong in the strict direction and the fog is drawn twice. Prefer the strict direction: a duplicate is visible and fixable, a silently merged activity is not — so an activity with **no distance is never deduplicated**, since type and start time alone are too weak to merge on.

**Every user-facing read excludes superseded rows** — the activity list, the summary, the histogram and its day pages, trends, the graph stats, the tracks tiles, and both fog/heatmap composites. The two reads that deliberately do not are the receive-time idempotency checks on `(user_id, source, external_id)`, where an already-superseded row is still "already processed", and the per-id lookups behind editing and deleting, which have to keep working for a duplicate the user can see.

**Deleting the winner re-ranks what it releases.** `superseded_by` is `ON DELETE SET NULL`, so every copy the deleted row displaced becomes live at once — without re-resolving, the same ride would be counted two or three times in the totals and drawn that many times into the additive heatmap, which is precisely what this section exists to prevent.

### 4.7 Activity listing and filtering

The Activities panel needs to browse, filter and multi-select a user's own history — a different, larger question than the endpoints around it answer. `GET /v1/activities/status/{external_id}` reports what happened to *one* upload; `GET /tiles/v1/tracks/{z}/{x}/{y}` renders geometry for whatever's in view. Neither lists activities as rows with metadata. The three endpoints below cover that, and all three are built — `apps/web`'s header, Activities panel and bottom timeline read them and nothing else.

```http
GET /v1/activities?from=2026-06-01&to=2026-09-18&types=ride,run
```

- **Filters**: `from`/`to` (inclusive date range) and `types` (comma-separated), both optional — an absent filter means "no restriction," the same convention §4.3's tracks query already uses for its own `from`/`to`/`types`.
- **Sort**: newest first.
- **No pagination.** See below for why.
- **Response**: `{"activities": [...]}`, where each row carries `id`, `started_at`, `activity_type`, `distance_meters`, `duration_seconds` and `bbox` — what the panel renders, plus the one thing it needs to point the map at a row. `bbox` is `[minLon, minLat, maxLon, maxLat]` (GeoJSON's ordering), computed per row with `ST_XMin(trajectory)` and friends, and `null` when the row has no trajectory. It is not a stored column: PostGIS caches a geometry's bounding box in its header, so reading it costs nothing like a scan, and §3.3's note on why the `BOX2D` column was removed applies equally to adding a new one. Full geometry still never travels this way — that is §4.3's tiles. **`duration_seconds`, not `moving_seconds`**: the ingest pipeline populates `moving_seconds` now (§4.5's ingest-gap fixes), but only for activities ingested since — anything older still has it `null`, and this list serves every activity through the same row shape, so it stays on `duration_seconds`, which has no such gap. `moving_seconds` is for callers that can accept that split, like §4.5's own trends. The metric fields stay nullable rather than defaulting to `0` — a file that carried no distance is not a zero-distance activity, and the UI shows an em dash for the difference.

**Built**, in `internal/httpapi/activities.go`. The `from`/`to`/`types` parsing is shared code with §4.3's tracks query (`parseActivityFilter`), so the "absent means no restriction" convention cannot drift between the map and the list.

```http
GET /v1/activities/duplicates
```

**Also built**, and it is the counterpart the list needs once a second ingest path exists: the list only ever shows live activities (§4.6), so something a user synced can be absent for two quite different reasons — it failed, or it was already here from somewhere else — and only the second is not a fault. Each row names both sources, since that is the actual answer to "why is this not on my map". No filter and no pagination: duplicates are a small set beside the history they came from, and the question is asked about all of them at once.

**Pagination was considered and dropped.** A row-wise keyset cursor, `(started_at, id) < ($cursor_ts, $cursor_id)` — chosen over `started_at < $cursor_ts` alone because duplicate start times are real here (the same ride ingested from two files lands as two rows sharing a `started_at`), and a timestamp-only cursor either drops or repeats one at a page boundary — isn't worth it: the Activities panel's TYPE and DISTANCE filters, computed client-side over the panel's own rows, need the *whole* current range at once (their checkbox counts and slider bounds), and the map already draws every matching track regardless of what the list had paged in, so a cursor was never actually letting anyone skip work. Phase 1 has one seeded user with activity counts in the hundreds to low thousands at most; a real multi-user phase is what's likely to make this worth revisiting.

**Revised: a name column, but a narrower one than the original idea.** An early design's row text ("Long ride — Karlštejn") implied an `activities.name` this schema didn't have — rather than add one plus the per-format parsing work to populate it (GPX's optional `<name>`, TCX's `<Notes>`, FIT's string fields, and a fallback for the very real case where the source carries none), the list showed `started_at` instead, for the reasons the original version of this paragraph gave. That still holds for *parsing* a name out of a source file — none of the three parsers read one, and this revision doesn't change that. What changed is allowing a name at all: `migrations/0015_activity_name.sql` adds `activities.name VARCHAR(200)`, nullable, set only by a person via `PATCH /v1/activities/{id}` (§4.7.4) — the same mechanism `description` already uses, not a new one. A row with a name shows it as the primary line; a row without one still falls back to the full start datetime exactly as before. Sorting is unaffected either way — `listActivitiesQuery`'s `ORDER BY started_at DESC, id DESC` never looked at this column and still doesn't.

Two more things the screen needs that a list endpoint alone doesn't cover. They are separate endpoints, as expected — their query shapes have nothing in common with a paginated list, or with each other:

- **The range summary** ("1,284 km · 186 activities · 42 h · 9,120 m up") — an aggregate (`SUM`/`COUNT`) over the same filter, not per-row data.

  **Built**: `GET /v1/activities/summary?from=&to=&types=`, taking exactly the filter shape the list takes — the totals have to describe the same set of activities the list is showing, which only holds if both read the same filter through the same parser. One query, one row: `{"count", "distance_meters", "duration_seconds", "elevation_gain_m"}`. These `COALESCE` to `0`, where the list's per-row metrics stay null, because the two nulls mean different things — "this activity recorded no distance" is genuinely unknown, while "these activities add up to nothing" is a real zero.

  **Default range: the 5 most recent activity-days, not all-time.** An all-time default would span every bar currently loaded, pinned to both edges with nowhere to slide, so the first thing a new user might try (dragging the band) would visibly do nothing. Five recent days starts the band well inside both edges instead, so sliding it works the first time it's tried. The list, the header badge, the panel's "km loaded" subtext, the summary line and the histogram's band all still read the same two dates from one place (`apps/web/src/map/MapView.tsx`'s `selectedRange`) rather than each deciding independently what the active range is, and it's all still **UTC**, matching the histogram's UTC-day buckets below. All-time is still one drag away — widen either handle past the loaded edge and the picker prefetches more history — it's just no longer where a first-time visitor starts.

  Changing the range clears the row selection, the focused row, the hidden-track set, and the TYPE/DISTANCE filters (§4.7.1 below) together — all could otherwise silently describe activities the new range doesn't list, and a stale DISTANCE band in particular could exclude everything if the new range's real min/max don't overlap it.

  **The effect computing the default "5 most recent activity-days" (`MapView.tsx`) must re-derive when an account's activity data actually changes, not run only once.** Guarding purely on `selectedRange !== null` would freeze a zero-activity account's degenerate `{today, today}` fallback (every demo account, on first load — §4.10, since `recentDays[0]` is `undefined` with no activity-days to slice) forever — that value still satisfies the guard, so a real upload landing afterward would never trigger the effect to reconsider it.

  Re-guarded on a `userChangedRangeRef` instead — set only inside `changeSelectedRange` (a real, deliberate drag/click), so the effect keeps reconsidering the default for as long as the user hasn't actually chosen one themselves, however many uploads land in the meantime. It can't simply depend on `visibleDays` directly to notice new data, though: panning (Earlier/Later, dragging the strip) changes `visibleDays` exactly as much as a genuine upload does, and must never re-pick the selection out from under someone who's just browsing, not uploading. `useActivityDays.ts` now exposes `generation` — its internal reload nonce, bumped only by `reload()` (an actual refetch, called after an upload) and never by `panBy` (which only moves an anchor over already-loaded data) — and the effect depends on that instead, reading `visibleDays` fresh from the closure without it being a dependency at all. Verified live, in both directions: a brand-new demo account uploading a file dated weeks in the past now sees it appear with no reload, while a real account that has already dragged the band to a deliberate range keeps it exactly as set through both a pan and a subsequent upload.
- **The daily histogram** (Jan–Nov bars) — a per-day (or per-week) count/distance aggregate across the *whole* visible timeline, independent of whatever sub-range is currently selected. A `GROUP BY date_trunc(...)` query, unrelated in shape to the other two.

  **Built**: `GET /v1/activities/histogram`, taking neither the list's filter nor a hardcoded window, in two modes — `?days=&before=` (activity-day pages, what the picker reads) and `?from=&to=` (a calendar window, the default); see the two notes below. Still no `types` filter: this chart is the backdrop a selected sub-range is highlighted *against*, so narrowing it by the same filter as the list would leave nothing to highlight against.

  **Resolved: the bucket is a day**, `date_trunc('day', started_at AT TIME ZONE 'UTC')`, not a week. Specifically a **UTC** day: `users` has no timezone column to bucket by (§4.9's accounts didn't add one — out of scope for "simple email+password"), and bucketing in the server's local zone would make identical data bucket differently depending on where the server runs. Worth revisiting now that there's a real per-user row to hang a timezone preference off of, since a late-evening activity can land on the next UTC day from the one its own row displays.

  **A panning window, not a fixed year.** An unbounded "whole timeline" would make both the query and the rendered month axis grow forever, so `from`/`to` are optional query params, defaulting to the trailing twelve months when both are absent, and the response always carries `earliest` — this user's first activity's UTC day, computed independently of the requested window via a cheap indexed `MIN(started_at)` — so the client knows where panning has to stop.

  Only days that have activities come back: `GROUP BY day` produces no row for an empty day, so the response stays proportional to what a user recorded rather than to how long the window is.

  **The strip pages by activity-day, and the endpoint has a second mode for it.** A slot per calendar day, empty days included as zero-height gaps, would spend most of the strip's width drawing nothing on a real history, so the picker renders **one bar per day that has activity, packed** — adjacent bars are adjacent *recorded* days, whether their real dates are a day or a year apart (see "no proportional axis" below for what that costs).

  That changes what a request means. "One screen of bars" is now a count of real days, not a width of calendar time, and a calendar window can't express it: with a sparse history a client would have to guess a `from`/`to`, count what came back, and widen the guess until it had enough — several round trips per pan step, and a differently-wrong first guess for every user. Both options were on the table and the widening loop is the one that was rejected. So:

  ```http
  GET /v1/activities/histogram?days=45&before=2025-06-30
  ```

  returns the `days` most recent distinct days-with-activity strictly before `before` (omit `before` for the most recent overall), ascending, same body as the other mode — one page is exactly that many bars however sparse the history is, no widening. `earliest` is reused unchanged as the stop condition: paging back has arrived at the beginning when the oldest bar in hand *is* `earliest`. There is deliberately **no `after=` counterpart** — a client starts at the newest end and only ever walks backwards, so everything between its pan position and today is already in hand by construction.

  The `from`/`to` calendar-window mode stays, and stays the default: §4.8's year-grid activity graph lays days out against real elapsed time and is the caller that wants it. The anchor filters `started_at` rather than the grouped `day`, so it stays on `idx_activities_user_time`.

  **No proportional axis, so no evenly-spaced month labels.** Placing ticks at their fraction of the window's elapsed time would, with packed bars, assert an even spacing through time that isn't there. A label is drawn under the first bar of each month, at that bar's own slot position, and dropped when it would land too close to the previous one — so a quiet year and a busy year visibly occupy the same width while their labels don't pretend otherwise. The per-bar hover tooltip carries the exact date, count and distance.

  **The mockup's highlighted selection band draws the active range on top of these bars, and is draggable.** The band is clipped to what is currently on screen, since the two are independent — the pan position is whichever bars are drawn, the selection is whatever the picker says — and one reaching past either edge starts at that edge rather than off it. A selection that is entirely off screen simply isn't drawn until it is paged back to; the footer's own range label says what is selected the whole time. Selection edges snap to activity-days, because with the empty days gone there is nothing between two bars to select. Note that sliding the band preserves its width in *bars*, not in calendar days.

  The bars stay whole-timeline at every setting. That is the point of drawing a band rather than filtering the chart — the histogram shows the whole picture with the selection marked on it, not a view that has thrown the context away.

  **No presets, direct manipulation.** Drag either handle to resize the selected range, drag the highlighted band itself to slide it, and page through history with the **Earlier**/**Later** buttons or by dragging the strip itself — built in `apps/web/src/ui/RangePicker.tsx`, over `apps/web/src/ui/useActivityDays.ts`, which owns the loaded run of activity-days and the pan position within it. Panning never writes to the selection: paging all the way back to a user's very first activity leaves `selectedRange` exactly as it was. Clicking a bar to filter to that one day is still separate work.

  **The whole band is a slide target, and hover still reaches every bar underneath it.** The band stays `pointer-events: none` (so a covered bar's own hover tooltip isn't swallowed by it), and "is this pointerdown inside the band" is decided in JS instead of CSS — `RangePicker.tsx`'s `onChartPointerDown` compares the click's pixel position against the band's own rectangle (excluding the month rail, which stays a pan target on purpose) and dispatches slide-vs-pan itself. Confirmed directly with `elementFromPoint` at the band's own center that it resolves to the bar underneath, not the band.

  **`beginDrag`'s pointerdown handlers call `event.preventDefault()` (pan, slide, and both resize handles, since all four go through it), plus `user-select: none` on `.range-picker__chart` as a second line of defense.** Without it, a pan drag that crosses the month labels' text lets Firefox start its own "select this content" gesture; a later drag — a handle included — then gets reinterpreted as dragging that native selection instead of being delivered as `pointermove` events, so resize logic never runs. Chromium doesn't trigger this as readily. Verified in Chromium via the existing regression suite; not independently re-verified in Firefox itself (unavailable in this sandboxed dev environment) — this is the standard, documented fix for the class of bug.

  **Bar height is log-scaled against the busiest day on screen, not linear.** A linear scale makes every bar's height *proportional* to the peak rather than merely *ordered* by it, so one outlier day (a big multi-activity ride) flattens every ordinary day on the same strip down to the visibility floor. `height% = log(distance + 1) / log(peak + 1)` keeps the same ordering — the busiest day is still tallest — while compressing the range so a quiet day stays visibly a bar rather than a hairline. Confirmed directly: a 0.9 km day that rendered under 10% tall on the old linear scale renders past 60% on the log one, on the same data, without changing which bar is tallest.

  **The footer is a legend column beside a chart area, not a header row above a full-width chart** (the mockup-driven redesign). The legend column is sized to its own content (`flex: none`, no explicit width) — an earlier pass matched it to the Activities panel's own `var(--panel-width)` so the two stayed visually aligned, but that meant the date picker's own usable width shrank or grew along with an unrelated sidebar drag, which the user asked to undo directly: the sidebar's width (independently resizable, 260–560px) has no bearing on how much room the chart area beside it should get. The chart area (`flex: 1`) takes whatever the legend doesn't need, so it gets as much room as it can regardless of sidebar width. Inside the legend, two title/subtitle blocks stack vertically as one column, not side by side — a side-by-side split (with a vertical divider between them) was tried first and found live to leave each date range only about half the column's width to render in, truncating mid-number well before the text actually ran out of room to matter. Text is right-aligned and set smaller than the rest of the panel's type scale (an explicit follow-up ask, not left over from an earlier pass). SHOWN DAYS sits on top, naming the *pan window* — a different question, since it can show a completely different stretch of history than what's selected; SELECTED RANGE sits below it, naming the *selection* (its date range, then how many of its days actually have activity — `"N-day range · M active days"`). Neither repeats km/activity-count/duration/elevation: those already live in `ActivitiesPanel`'s "km loaded" subtext and ACTIVITIES badge (§4.7.1 below is the filtered version of the same count).

  **Label reads "M active days", not "M with activity".** `M` is a count of *days*, computed the same way the badge and row list count activities — worded this way since at `M === 1` the two would otherwise read as indistinguishable (a single day with 2 activities on it would look like an activity count of 1).

  **The chart area fills the rest of the row, Earlier/Later flanking `<RangePicker>` directly rather than sitting in the legend.** Icon-only now, and restyled to look actively clickable when `canPanEarlier`/`canPanLater` allow it and flatly inert when they don't — the same booleans as before, gating a `disabled` attribute exactly as they always did, just with a real visual distinction between the two states rather than relying on the browser's default disabled dimming alone. `RangePicker.tsx` still only takes the `onPan` callback its own drag-the-strip gesture needs; `ActivityHistogram.tsx` still renders the buttons. Both the buttons and `.range-picker__chart` have no explicit height at all on desktop — `.activity-histogram__chart-area`'s `align-items: stretch` fills them to its own full height (minus a small 6px vertical padding, confirmed live as the fix for an earlier pass that centered a fixed 56px strip inside far more vertical space than it used, leaving distracting empty margins above and below), a chain that only resolves because `.activity-histogram` itself has a definite `height: 104px` for the stretch to originate from. The buttons are also thinner now (19px, was 34px) — proportioned for a strip this much taller, not squat next to it.

  **A month label positioned right at the chart's own left edge overlapped the Earlier button beside it — found live logged in as the demo account, which seeds enough history for exactly that to happen.** `.range-picker__month`'s `transform: translateX(-50%)` centers a label on its bar's position, so a label for the *earliest* bar in view — with nothing to its left inside the chart at all — has roughly half its own text width extending past the chart's own boundary by design; that overflow used to render on top of whatever sat outside the chart entirely, which for most of this feature's life was empty space, but now is the Earlier button, 10px away. `overflow: hidden` on the month rail was tried first and rejected on the spot: it stopped the overlap but also cut the label down to an unreadable "…2026", trading one visible problem for a worse one. The actual fix is in `monthTicks()` (`RangePicker.tsx`): a tick within `EDGE_ALIGN_THRESHOLD_PERCENT` (4%) of either edge gets `align: 'start'`/`'end'` instead of `'center'`, which `.range-picker__month--start`/`--end` (index.css) render as `translateX(0)`/`translateX(-100%)` — anchoring the label's own edge to the tick instead of its center, so the full text stays on screen and inside the chart's own bounds. Every other tick, including a short history's own first one sitting well inside blank space (a nonzero `blankFraction`, nowhere near either edge), keeps centering exactly as before — confirmed live, not just reasoned about, that this only changes behavior for a tick actually at risk of overflowing.

  Mobile can't rely on the same stretch chain (`.activity-histogram` is `height: auto` there, stacking legend above chart-area instead of a fixed-height row — with no definite height anywhere in that chain, `.range-picker__chart`'s own children are all `position: absolute` and contribute no intrinsic height, so it would collapse to 0px), so the mobile media query keeps an explicit fallback height (64px) on both elements instead.

  **Filled the last documented gap: clicking a bar selects that one day.** Bars themselves carry no click handler — `onChartPointerDown` already hit-tests pan vs. slide on every pointerdown, so a click is recognized the same way, in `endDrag`: a pointerdown/up pair that started as a pan (missed the band) but ends within a few pixels of where it started *and* never actually panned (`drag.panned === 0`) selects the day under the release point instead of leaving a pan gesture that went nowhere. Both conditions are required together — the pixel check alone would misread a real pan across many bars narrow enough to round to zero net slots as a click, and the `panned === 0` check alone would misread a few pixels of incidental jitter (trackpad, touchscreen) as an intentional drag. Clicking inside the current band still slides it, unaffected — that path is a different drag mode entirely.

#### 4.7.1 TYPE and DISTANCE filters

The Activities panel has two filters: TYPE rows (with per-type counts) and a DISTANCE dual-handle slider bounded by the range's real minimum and maximum. **Built, entirely client-side** (`apps/web/src/ui/activityFacets.ts`), over the same unpaginated list response the panel already holds for the current date range — no new endpoint, no new query parameters. Narrowing either updates what the panel renders, what the map draws (unioned into the same track-hiding filter the eye icon uses), and the ACTIVITIES badge's count; it does not change the "km loaded" subtext, which still describes the whole date range regardless of either filter. "Reset filters" clears both instantly, since there is nothing to refetch. TYPE labels are humanized for display (`snake_case` spaced out and title-cased) but never remapped to a fixed vocabulary — `activity_type` "is not a controlled vocabulary" below still holds; formatting is not normalizing.

TYPE rows are real checkboxes in a scrolling list (§4.7.6), not a fixed category list — rows show only values actually present in the current range's own data, never a hardcoded vocabulary.

#### 4.7.2 `activity_type` is not a controlled vocabulary

Revising §3.3's comment: `activity_type` is whatever the source reports, not the small fixed set the old `-- 'run' | 'ride' | 'hike' | ...` comment implied. Garmin/FIT's `sport`/`sub_sport`, TCX's `Sport` attribute, and GPX's optional `<type>` element each use their own vocabulary, and values like "Gravel" existing alongside "Ride" from the same source are real, not a data error. The product's answer: show it, don't normalize it — a filter list is built from whatever distinct values a given user's own activities actually contain, never a hardcoded set.

**Resolved for all three formats**, verified directly against `internal/parse` and against a live upload through the real HTTP endpoint (not just unit tests) for GPX and FIT — TCX's live path was already covered. GPX reads its optional `<trk><type>` element (`internal/parse/gpx.go`). FIT reads the `session` message (global message 18, not `record`) and resolves `sport`/`sub_sport` (fields 5/6) through `internal/parse/fit_sport.go`'s lookup tables — transcribed programmatically from python-fitparse's `profile.py` (itself generated from Garmin's own FIT SDK `Profile.xlsx`), not typed by hand, since a wrong numeric mapping in ~110 enum values is exactly the kind of error that can't be caught by inspection. `sub_sport` wins when it says something `sport` doesn't (`gravel_cycling` over plain `cycling` — confirmed live: a real `.fit` upload with sport=cycling, sub_sport=gravel_cycling lands as `activity_type = 'gravel_cycling'`), falling back to `sport` when `sub_sport` is `0` ("generic") or unset.

#### 4.7.3 Row interactions and a resizable panel

Each row (`apps/web/src/ui/ActivitiesPanel.tsx`) has four independent interactions: hovering it, clicking its text, its checkbox, and its eye icon.

**Row-click focus and the checkbox group are two independent pieces of state, not one shared `Set`.** `MapView.tsx` keeps `checkedActivityIds` (the checkbox group, additive/subtractive, `toggleActivityChecked`) and `focusedActivityId` (the row-click focus, `string | null`, replace-on-click, `focusActivity`) as two separate `useState`s that never write to each other in either direction:

- **Hover** (`onMouseEnter`/`onMouseLeave` on the row) previews that one track — bolded on the map via the `hover` feature-state, camera untouched, cleared the instant the pointer leaves.
- **The row's text** (`focusActivity`) *replaces* `focusedActivityId` with just that one id and flies the camera to it immediately — clicking a different row simply replaces the value, dropping the previous row's focus highlight for free.
- **The checkbox** (`.activities-panel__checkbox`, `toggleActivityChecked`) *adds or removes* just that id from `checkedActivityIds` — the multi-select mechanic. This group flies via a debounced effect (`SELECTION_FLY_DEBOUNCE_MS` = 300ms) keyed on `checkedActivityIds` alone, so checking a box never re-flies to wherever the focused row happens to be, and vice versa.
- **What's bold on the map** (`tracks.ts`'s `setSelectedTracks`) is the union of the two (`boldedActivityIds`, `useMemo`) — a row checked *and* currently focused still renders as one indistinguishable bold, not two competing states. Each row's own `.activities-panel__row--selected` class reads the same union (`checked.has(id) || focusedId === id`).
- Colored zone segments (`trackBands.ts`, §4.3.1) key off `focusedActivityId` directly — checking a box alone never shows bands, only a row click does.

**Hover is centralized in `tracks.ts`** so the map's own mousemove-driven hover and a row's own hover apply the same feature-state through one function, `setHoveredTrack(map, activityId | null)` — the map's mousemove/mouseleave report up through an `onHover` handler (mirroring `onSelect`) into `MapView.tsx`'s `hoveredActivityId` state, and a single effect is the only caller of `setHoveredTrack`, fed by both a row's hover and the map's own. Centralizing it there is what stops the two input sources from clobbering each other's idea of what's currently hovered.

**`Clear` flies to fit everything visible.** `clearSelection` in `MapView.tsx` empties `checkedActivityIds` and calls `fitToSelection` over every currently-visible activity (excluding `mapHiddenIds`, the same exclusion the checkbox group's own auto-fly and the band-change auto-fly apply, for the same reason — flying to something not drawn looks like flying to empty water). Immediate, not debounced, since it's one deliberate click rather than a multi-row spree that needs settling.

The row highlight is one blue treatment (`.activities-panel__row--selected`, with the left stripe) regardless of which mechanism put a row in the union — there is only one kind of "selected" a row can be, with two independent doors into it.

**The panel is resizable, not a fixed 320px** — a full timestamp title ("Sep 11, 2026, 09:00 AM") plus its TYPE label needs the room. `panelWidth` is local state in `ActivitiesPanel.tsx` (260–560px, default 380px) rather than lifted to `MapView.tsx`, on the same reasoning as `filtersOpen` — nothing outside this component's own layout needs to know it — and isn't persisted across reloads, since no other UI preference here is either. The drag handle uses Pointer Capture (`setPointerCapture`), the same pattern `RangePicker.tsx` uses for its own drag, so the drag keeps tracking if the cursor leaves the narrow hit target mid-gesture. One gotcha worth not rediscovering: the handle has to sit fully inside the panel's own box (`right: 0`), not straddle its edge with a negative offset — the panel has `overflow: hidden` for its scrolling list, which clips (and makes unclickable) anything positioned outside that box.

#### 4.7.4 Edit activity type, name, and description (FR-5.10)

**Built directly on §4.7.2's own resolution, not in tension with it.** §4.7.2 already decided `activity_type` is whatever the source reports, not a controlled vocabulary — a user-typed rename is architecturally identical to what ingest itself already writes there, so nothing about that decision needed revisiting to let a user edit it after the fact. `description` is a nullable `TEXT` column on `activities`, free text — longer-form, shown only as a hover tooltip, never the row's visible title. `name` (`migrations/0015_activity_name.sql`, `VARCHAR(200)`, nullable) is a later revision of §4.7's original "no name column" call, and stays deliberately narrower than what that section rejected: a name exists only once a person types one in here, exactly like `description` — no parser reads one out of a source file, and adding this column didn't reopen that question. A row with a name shows it as the primary line in place of `started_at`; a row without one still falls back to `started_at` exactly as §4.7 originally specified.

**`PATCH /v1/activities/{id}`** (`internal/httpapi/activities.go`'s `handleUpdateActivity`) follows `internal/httpapi/account.go`'s `handleUpdateSettings` shape rather than inventing a new one: full-replace-on-save (one Save button commits `activity_type`, `name`, and `description` together, not per-field), `NULLIF($n, '')` for "empty name/description means cleared," and ownership-scoped exactly like `track-metrics/{id}` (`WHERE id = $1 AND user_id = $2`, rows-affected checked to return a `404` indistinguishable from "doesn't exist" for a non-owned id). `activity_type` has no such empty-means-unset escape hatch, since the column stays `NOT NULL` — an empty value is rejected with a `400`, not silently kept unchanged; `name`, like `description`, is optional and may be cleared. `maxActivityTypeLen` (50, matching the column's own `VARCHAR(50)`), `maxActivityNameLen` (200, matching `name`'s own `VARCHAR(200)` — a single-line title bound, deliberately shorter than description's), and `maxActivityDescriptionLen` (2000, a generous-but-real free-text bound, the same "reject an obvious mistake, not opine on reasonable length" reasoning `account.go`'s `maxPrivacyTrimM` already uses) are all enforced server-side and mirrored client-side in the dialog, so a caller sees the limit before submitting, not only after a rejected request.

`listActivitiesQuery` and the new single-row `activityByIDQuery` share one `scanActivityRow` helper (same column order, same bbox-from-four-nullable-floats reconstruction) rather than duplicating the scan — the endpoint that lists activities and the endpoint that returns one freshly-edited activity were always going to need the exact same row shape.

**Frontend: `EditActivityDialog.tsx`, reached from the header toolbar's Edit icon (`.activities-panel__edit`) over whatever is currently checked (FR-5.6) — there is no per-row edit control.** Its `activities` prop is always an array, never a single `Activity`, and it branches on length: exactly one checked activity edits Type, Name, and Description together, the same as before this became a toolbar action; more than one checked edits **Type only**, since there's nothing consistent to set across several different activities' names/descriptions in one request — the Name and Description `<input>`/`<textarea>` render `disabled`, each with a `title` tooltip explaining why, rather than silently doing nothing or (worse) overwriting every checked activity with one shared name. Saving a single activity is one `updateActivity` call; saving a group is one call **per checked activity, sequentially** (the same "hand-picked group, not bulk-import scale" reasoning §4.7.5's delete already uses) — and since the endpoint is a full replace, each of those per-activity calls resends that activity's own current `name`/`description` unchanged alongside the new shared `activityType`, or the backend would silently clear them. Confirmed directly with the user rather than assumed: the description shows as a **hover tooltip only** (the row's own `title` attribute — no second visible line, matching the existing `RangePicker`-bar-tooltip precedent for "extra detail on hover" elsewhere in this app). The dialog itself reuses `PlaceholderNotice.tsx`'s native `<dialog>`/`showModal()` pattern (the only existing "small focused form over the map" primitive here, chosen for its free Escape/backdrop/focus-trap behavior) rather than `UploadPanel.tsx`'s hand-wired anchored dropdown, which duplicates its own outside-click logic per component today instead of being a shared hook. The Type field's `<datalist>` suggestions are `facets.map(f => f.type)` — the same set §4.7.6's header toolbar builds its own TYPE checkboxes from (`activityFacets.ts`) — reused as-is rather than recomputed, and purely a convenience: nothing is enforced against it, so a value nobody has used before ("Solowheel", "Roadtrip") saves exactly as typed.

**On save, the caller just calls `useActivityList`'s `reload()`** (`MapView.tsx`'s existing `reloadActivities`, passed down as `onActivityUpdated`) rather than patching the local array in place — the same "just refetch" convention an upload completion already uses (`handleUploaded`). This list is small (hundreds to low thousands of rows per account, per §4.7's own scaling note), and a full refetch is simpler to reason about than optimistic patching at that scale. Once it lands, `activityFacets.ts`'s TYPE chips, the row's own label (`activity.name` when set, `started_at` otherwise — `ActivitiesPanel.tsx`), and the hover tooltip all recompute for free from the refreshed `activityType`/`name`/`description` — no changes needed to the filter mechanism itself, confirming §4.7.2's design was already general enough for this. Sort order is untouched regardless of what a row displays: `listActivitiesQuery`'s `ORDER BY started_at DESC, id DESC` never reads `name`.

**Explicitly unaffected, and verified live, not just reasoned about:** Trends is already type-agnostic (§4.7.2) — renaming `activity_type` never triggers or needs a recompute of it. Fog of War and Heatmap key off trajectory and activity id, never `activity_type` — switching to Fog mode immediately after a rename renders that activity's coverage exactly as before. Re-upload/dedupe is a pure no-op on an existing row (`ON CONFLICT ... DO NOTHING`), so a manual edit can never be silently overwritten by re-syncing the same file later.

#### 4.7.5 Delete an activity — a full purge, not a soft delete (FR-5.11)

**Most of "purge everything" is free.** `activity_streams` and `activity_tile_masks` are both `ON DELETE CASCADE` back to `activities.id` (confirmed by reading every migration, not assumed), so a plain `DELETE FROM activities WHERE id = $1 AND user_id = $2` removes both along with the row itself. Trends is live-aggregated (`SUM`) over whatever rows remain at query time, not cached anywhere. Object storage has no foreign keys, so it's the one thing the cascade can't reach: `activity-masks/{activityID}/` (uniquely scoped by activity UUID — safe and complete to `RemoveByPrefix`) and the raw upload blob both need explicit cleanup, following `demo_purge.go`'s existing shape (best-effort, logged, non-blocking, storage cleanup before the DB delete). One real edge case worth handling rather than documenting away: `raw_payload_key` (`raw/{userID}/{sha256(bytes)}{ext}`) is content-addressed but not source-scoped, so two distinct activities can share a key if their raw bytes are byte-identical (e.g. the same file ingested once via plain upload and once via a Takeout import) — `handleDeleteActivity` checks `SELECT EXISTS(... WHERE raw_payload_key = $1 AND id != $2)` before removing the blob, so a sibling activity's still-referenced raw file is never deleted out from under it.

**The one piece that wasn't free, and the reason this needed real investigation before being built:** the *unfiltered* Fog/Heatmap view reads a precomputed per-user, per-tile cache (`fog_tiles` + its object-storage PNGs) that `internal/fog.RenderUser` builds by fully recompositing from whatever `activity_tile_masks` rows currently exist for a tile — but the only thing that ever marks a tile dirty or enqueues that render is ingest (`ingest.MarkFogTilesDirty`/`ingest.EnqueueRenderFog`, both now exported specifically so this handler could reuse them rather than duplicate their SQL). Nothing about deleting an activity would, on its own, tell that cache anything changed — the cascade removes the `activity_tile_masks` rows, but the already-rendered cache PNGs would keep showing that activity's coverage as a stale artifact indefinitely. The fix needs no new compositing logic, because `RenderUser`'s own render is already "idempotent and complete per tile, not incremental" (its own doc comment) — it fully re-derives a tile from whatever masks exist *now*, so simply re-triggering the same dirty-mark-and-enqueue machinery ingest already uses, this time from `handleDeleteActivity`, produces a correct result. Concretely: `activityFogTiles` reads back the deleted activity's own touched z14 tiles from `activity_tile_masks` (its primary key leads with `activity_id`, so this is an index-only lookup) *before* the cascade removes that information, then — after the `DELETE` — those tiles are marked dirty and one `render_fog` job is enqueued for the user, same as a fresh upload triggers. This one trigger fixes both rasters: `renderAndStoreTile` (§4.2.3) rebuilds `object_key` and `heatmap_object_key` together from whichever masks remain, so a delete needs no raster-specific handling of its own.

**Verified live, not just reasoned about — the critical check:** uploaded two activities with overlapping z14 tile coverage, confirmed both visible on the unfiltered Fog tile, deleted one, and confirmed the served tile PNG actually changed (286 bytes differed, localized to where the deleted track had been) rather than staying stale. Deleted the second, and confirmed the tile became a single uniform fog color across all 262,144 pixels — exactly what a tile with zero activity coverage renders as, matching a never-touched tile. Also confirmed: the DB cascade (`activities`/`activity_streams`/`activity_tile_masks` all gone), a `404` for an already-deleted id, a bogus id, and another account's activity (never a `200` leaking whether it exists), and that a sibling activity survives a delete untouched.

**Frontend: the header toolbar's Delete icon (`.activities-panel__delete`) is the only delete entry point — there is no per-row delete control.** Deleting one activity means checking just its own box first, the same as any other single-item action since the per-row Visible/Edit/Delete icons were retired in favor of check-then-toolbar throughout (`ActivitiesPanel.tsx`). Confirmed directly with the user, not assumed: a delete has no undo, so clicking Delete opens a `ConfirmDialog.tsx` — deliberately generic, not delete-specific, reusing the same `<dialog>`/`showModal()` shape `PlaceholderNotice.tsx`/`EditActivityDialog.tsx` already established, so a future destructive action can reuse it rather than growing its own confirm UI. On confirm, `MapView.tsx`'s `handleActivitiesDeleted` reuses `handleUploaded` verbatim (deleting changes distance/duration totals and the histogram, unlike editing, which needs only a plain `reload()`), and additionally clears `focusedActivityId` if it pointed at any now-deleted activity — left dangling, the colored-zone-segments/pace-profile effect would keep trying to fetch `track-metrics` for an activity that no longer exists. `checkedActivityIds`/`hiddenActivityIds` are cleared too for tidiness, though a dangling id in either is already harmless (both are filtered against the freshly-reloaded activities list wherever they're read). Always takes an array of ids, even for a group of one — there is no separate single-id variant any more (the thin `handleActivityDeleted(id)` wrapper that used to back a per-row icon has been removed as dead code), so one combined refresh happens regardless of how many activities were deleted at once.

#### 4.7.6 Header toolbar — select-all, Type dropdown, group actions (FR-5.2/FR-5.7/FR-5.12/FR-5.13)

**Every single-item action lives in this toolbar — rows carry no action controls of their own at all** (the mockup-driven redesign). A row is only its checkbox and its text; editing, deleting, or hiding one activity means checking just its own box first, the same as acting on a group. This retired the per-row Visible/Edit/Delete icons entirely, along with the column-alignment scheme that used to keep this toolbar's controls lined up under them — with no row-level columns left to align to, the toolbar is now a plain compact strip, not a header mirroring the row's own flex geometry.

**Distance is its own standalone, always-visible section, not folded into the Type dropdown** — a control reachable without a click shouldn't be buried behind one. `DistanceFilter.tsx` (renamed from `ActivityFilters.tsx`, which now does the one job its name implies) renders standalone between the subtext line and the toolbar, guarded by the same `bounds && bounds.min < bounds.max` check as before — returns `null` rather than rendering anything when there's nothing meaningful to show a slider over.

**The toolbar, left to right**: the select-all checkbox; the Type dropdown trigger; a flex spacer (`flex: 1; min-width: 0`) pushing the rest right; three icon actions over the checked group — Show/hide (`.activities-panel__visibility`), Edit (`.activities-panel__edit`), Delete (`.activities-panel__delete`); a 1px vertical divider; and one accent-tinted Focus-on-map icon (`.activities-panel__focus`) set apart from the other three as the toolbar's primary, non-destructive action. All four icon buttons share one visual family (a tinted 30×30 chip) except Focus, whose accent tint marks it as the one action that isn't editing, hiding, or deleting anything.

**The Type dropdown holds an "All types" convenience row above the per-type checkboxes.** Its own checked state reflects `excludedTypes.size === 0`; clicking it loops `onToggleType` over every currently-excluded type, re-including all of them — a select-all, not a real toggle (it never excludes everything), consistent with how "Reset filters" elsewhere in this panel also only ever clears forward, never the reverse. Below it, TYPE's own checkbox-list markup (`.activity-filters__type-list`/`__type-item`, `checked` meaning "shown", scrolling via `max-height: 12rem; overflow-y: auto`) is unchanged, inline in `ActivitiesPanel.tsx` itself. `.activities-panel__type-panel` anchors `right: 0` under its trigger, keeping the dropdown from overflowing the panel's own narrow end.

**The header's master checkbox is a real tri-state control, not two swapped button labels.** `allChecked`/`someChecked` are computed from `checkedActivities.length` against the currently *listed* (already TYPE/DISTANCE-filtered) `activities.length` — `indeterminate` has no JSX prop (it's a DOM property, not an HTML attribute), so a ref + a `useEffect` sets it imperatively whenever `someChecked` changes. Clicking it when unchecked-or-indeterminate calls `onSelectAll`; clicking it when fully checked calls `onClear`.

**Group visible (FR-5.12) — confirmed directly as a toggle, not an "isolate" pattern.** The alternative considered was showing only the checked group and hiding everything else, a "solo" pattern from layer-based tools — rejected because it also changes *unchecked* rows' state, a bigger behavior change than this needed to be. `MapView.tsx`'s `toggleGroupVisibility`: if any checked id is currently in `hiddenActivityIds`, remove every checked id from it (show the whole group); otherwise add every checked id (hide the whole group). This is FR-5.8's entire hide/show mechanism now, not a bulk version of a separate per-row toggle — `tracks.ts`'s layer-filter application and Fog/Heatmap's hidden-activity exclusion (§4.2.3) read `hiddenActivityIds` exactly as before, unaware the trigger changed. A hidden row's title/meta text dims (`opacity: 0.55`) in place of the eye icon it no longer has, so hidden state is still visible at a glance.

**Delete (FR-5.13, unified with FR-5.11) reuses the same `DELETE /v1/activities/{id}` per activity, not a new bulk endpoint.** `ActivitiesPanel.tsx`'s confirm handler awaits each `deleteActivity(id)` call **sequentially**, not `Promise.all` — a checked group is a handful of rows a user selected by hand, not a bulk-import-scale operation, so there's no latency reason to parallelize, and sequential avoids N concurrent deletes each independently marking/enqueueing against the same user's `fog_tiles` rows at once. Once every delete lands, one call to `onActivitiesDeleted(ids)` (§4.7.5's `handleActivitiesDeleted`) does the combined refresh — never one refresh per deleted activity, even for a group of one. The confirm message pluralizes correctly for a one-activity group ("its track…") versus a larger one ("their tracks…").

**Edit (FR-5.10) is a real bulk capability behind this same toolbar icon, not a relocated single-row button** — see §4.7.4 for the exact single-vs-group split.

**Focus on map (FR-5.7) replaced the footer's old "Show selected" text button.** Same fly-to-fit-checked-group action (`onShowSelected`), same disabled-when-nothing-checked gating, just moved into the toolbar as an icon and set apart with a divider and an accent tint — the footer now holds only the "N selected · X km" summary text.

**Verified live**: checking 0/1/many rows correctly gates all four icon buttons' `disabled` state. Edit with exactly one checked opens the full dialog; with two or more, only Type is editable and saving left each activity's own name/description untouched. Delete's confirm dialog names the correct count/distance and singular/plural wording for both a group of one and several. Group visible hides/shows exactly the checked activities and dims their rows. Focus on map flies to fit the checked group with the footer's old button gone. The Type dropdown's "All types" row clears every exclusion in one click. Confirmed no horizontal overflow at a 390px mobile viewport, with the mobile-only touch-target sizing (34×34) applying to all four toolbar icon buttons.

### 4.8 Activity graph — a private, per-user contribution grid

**Built, single-user.** A GitHub-style daily contribution grid (`VISION.md` §4.2's "Activity graph" row) — one cell per day, a full calendar year per block, color intensity by that day's activity. Distinct from §4.7's bottom timeline in purpose, not just appearance: the timeline is an interactive filter that drives what the map and list show right now; this is a retrospective, whole-year-at-a-glance artifact — closer to a personal streak tracker than a control. There is no per-day click-to-filter here, and no RangePicker.tsx drag machinery reused — the whole interaction is look, not narrow.

**Scoped by `user_id` throughout, not single-user.** Every backend query below is scoped by a `user_id` parameter, so this works unchanged for the seeded placeholder account, a demo account, or a real registered account (§4.9) alike.

**Intensity is a toggle between count and distance, not a composite "effort" score.** A "SHADE BY" control switches the whole grid between the two, one at a time — it is not a blend (`ActivityGraph.tsx`'s `shadeBy` state, `YearGrid.tsx`'s `shadeLevel`). Folding duration, distance and "load" into one number is rejected for the same reason given elsewhere in this document — `training load` isn't built and isn't currently planned, so inventing a one-off formula here just to pick a color would mean redefining it twice. Count's three non-empty levels are the literal "one, two, three or more" the grid's own legend states; distance has no equivalent natural small- integer scale, so its levels are quantile thresholds (`distanceThresholds`) over that year's own active days — relative to what a *walker's* busy day looks like when shading a walker's year, and to a cyclist's when shading a cyclist's, rather than one fixed distance reading as "quiet" for one and "empty" for the other.

**Reuses `GET /v1/activities/histogram`'s day-bucket shape for the grid cells** — a plain year-scoped call to the existing endpoint (`?from=YYYY-01-01&to=YYYY-12-31`, `getActivityYearGraph` in `api.ts`), no new endpoint needed for the cells themselves. Its `earliest` field (added for RangePicker's pan-clamping, §4.7) does double duty here too: it bounds how many stacked year blocks `ActivityGraph.tsx` renders, from the current year back to the year of the user's first activity, so a history-less year is never drawn empty.

**One genuinely new endpoint for the stat cards.** The page shows four all-time stat cards — Activities, Distance, Active Days, Longest Streak — and repeats the same four as a per-year subtotal under each year's own grid, stacked most-recent-first (not a year switcher — every year renders at once, one block per year). Count and distance are trivial `COUNT`/`SUM` aggregates (`activityStatsAggregateQuery`, `services/server/internal/httpapi/activities.go`); **active days** is that same query's `COUNT(DISTINCT day)`, which the histogram query never had to expose as a single number before (a caller wanting it previously had to count buckets itself); **longest streak** — the longest run of consecutive calendar days with at least one activity — is real new work, a gaps-and-islands query (`activityLongestStreakQuery`): `day` minus its own row number (as whole days, `ROW_NUMBER() OVER (ORDER BY day)`) is constant across a run of consecutive dates and changes at every gap, so grouping by that expression groups exactly the consecutive runs, and the largest group is the longest streak. `GET /v1/activities/graph-stats?year=` runs both queries twice — once over `[year-01-01, year+1-01-01)`, once with no bound at all for all-time — rather than one combined query, since the streak needs its own CTE regardless and combining wouldn't save a round trip. Cross-checked directly against manual SQL over the seeded data (count, distance and active-days matched exactly; the longest-streak query was verified by hand against the same account's own sorted list of active dates) before wiring the frontend to it.

**An avatar, "Account since," connected-sources count, Edit profile, and Manage sources are all deliberately not built here** — real-accounts chrome, out of this section's scope. `ProfilePage.tsx` renders only the "ACTIVITY GRID" panel and its four all-time stat cards; that's this section's whole scope, not everything a full profile page could eventually hold. Showing a fabricated name or "account since" date here would be the same kind of dishonesty already rejected for Donate and the account menu itself (`UserMenu.tsx`) — a control drawn as if something real sits behind it when nothing does.

**Reached from the account menu's "Profile" item** — `UserMenu.tsx`'s Profile entry now navigates there directly instead of opening a placeholder notice; "Sign out" (§4.9) is real now too, and Donate is the only item still a placeholder. `App.tsx` switches between the map and this page with plain local state, not a router — there's no URL to bookmark or share for a page private to whichever account is signed in, and one extra screen doesn't justify a routing dependency. The header's brand mark becomes a "back to map" button only on this page (`Header`'s `onBrandClick`); on the map screen itself it stays inert, since there's nowhere more "home" to go from there.

### 4.9 Real accounts — simple email+password

**Built.** Email+password with server-side sessions, not a third-party identity provider — matching the no-revocable-vendor bias §4.0.2 already made for Google Takeout. `internal/httpapi/auth.go` holds this alongside §4.10's demo and §4.11's password recovery. `migrations/0006_sessions.sql` added the `sessions` table; `users.email`/`password_hash` already existed, unused, from the original seed migration.

**Endpoints**: `POST /v1/auth/signup`, `POST /v1/auth/login`, `POST /v1/auth/logout`, `GET /v1/auth/me`. Every other route is now wrapped in `requireAuth`, a middleware that resolves the session cookie to a `userID` and attaches it to the request context (`userIDFromContext`) rather than changing every handler's signature — a smaller diff than threading a parameter through roughly ten handlers and their registrations.

**Sessions are an opaque cookie, not a JWT.** `sessions(id UUID PK DEFAULT gen_random_uuid(), user_id, created_at, expires_at)` — the row's own primary key *is* the cookie value, no separate hash-of-token layer. Judged sufficient for this app's current single-user personal threat model; revisit the day that changes. 30-day TTL, checked in SQL (`expires_at > NOW()`) rather than trusted from the cookie's own stated expiry, so a replayed stale cookie past its expiry still fails. `handleLogout` deletes the row server-side, not just the browser's copy, so a captured-but-unexpired token stops working immediately.

**"Claim vs. create" was the original design decision, retired as part of the demo-account redesign (`docs/SPEC.md` FR-2.1–FR-2.3).** The very first signup ever (`SELECT EXISTS (SELECT 1 FROM users WHERE password_hash IS NOT NULL)`) used to update the seeded placeholder user's row in place — same `id`, new `email`/`password_hash` — instead of inserting a new one (`claimOrCreateUser`), so activity uploaded before accounts existed stayed attached to the same account with zero migration step. **Revised**: `handleSignup` is now always a plain `INSERT`, including the very first signup ever — the placeholder-row claim was retired along with the demo-account half of "claim vs create" (§4.10's own revision note), since keeping one narrow claim path for a seed row long since claimed (or never going to be, on any fresh deployment) wasn't worth the extra branch once the demo half of the same logic went away too. `PlaceholderUserID` no longer exists as a Go constant; the seed migration itself (`migrations/0003_seed_demo_user.sql`) is untouched, since migrations are never edited after the fact, but the row it inserts is now permanently unclaimed dead data on any database old enough to have it.

**Credentialed CORS.** Cookies require the server to reflect a specific `Access-Control-Allow-Origin` (not `*`, which credentials forbid) plus `Access-Control-Allow-Credentials: true` plus `Vary: Origin` — `corsAllowedOrigins` in `server.go` is an explicit allow-list of the origins expected to send credentialed requests (the dev server, and the two ports `apps/web/tests/smoke.mjs`/`build.mjs` spawn for themselves). A production origin gets added here the day one exists.

**MapLibre's own tile fetches needed separate credential wiring.** Fog, heatmap and tracks tiles are all fetched by MapLibre's internal machinery, which never goes through `api.ts`'s fetch wrapper — `useMapInstance.ts` supplies `transformRequest` to attach `credentials: 'include'` to those requests directly.

**Frontend**: `apps/web/src/auth/AuthContext.tsx` (a small Context so `UserMenu`, three component levels below `App`, doesn't need `user`/`signOut`/`requestUpgrade` prop-drilled through `MapView`/`ProfilePage` and `Header`) and `AuthGate.tsx` — the one shared "no confirmed session" screen for every case: sign-in/sign-up, a `resetToken`-driven "set a new password" screen (§4.11), and a "forgot"/"forgot-sent" pair (§4.11), rather than separate pages per flow. `App.tsx` models auth state as `'checking' | 'signed-out' | SessionUser` rather than `SessionUser | null`, specifically so "haven't asked yet" renders nothing instead of flashing the sign-in form before an existing valid session cookie is confirmed — `SessionUser` is `AuthUser | DemoUser` (`api.ts`), a discriminated union rather than a boolean `isDemo` flag; see §4.10 for why.

**Deliberately simple, matching the instruction that built it**: no OAuth. Password reset is built — §4.11. **Email verification is also now built** (`docs/SPEC.md` FR-1.8, `migrations/0016_email_verification.sql`): `users.email_verified` defaults `false`; `email_verifications` mirrors `password_resets`' shape (a `gen_random_uuid()` row id used directly as the token, 24h TTL — longer than a reset link's, since confirming a signup is less time-sensitive than a credential reset). `requireVerified` (auth.go) wraps every route that used to be plain `requireAuth`, returning `403 {"error":"email_not_verified"}` for a real, unverified account — a demo account always passes, since the gate was never meant for one (see §4.10's revision). `POST /v1/auth/verify-email` consumes the token and mints a fresh session exactly like `reset-password` does, so the link works regardless of which browser/device opens it. `POST /v1/auth/resend-verification` and `PATCH /v1/auth/email` are plain `requireAuth`, deliberately not `requireVerified` — they exist specifically to help an account that hasn't verified yet, and the latter resets `email_verified` to `false` and re-sends on any change, whether or not the account was already verified. `SKIP_EMAIL_VERIFICATION` (config.go) bypasses the whole gate at signup time — defaults `true` in local dev's `compose.yaml` for `apps/web/tests/smoke.mjs`/`build.mjs`'s fixture account (no mailbox to fetch a token out of in CI), always `false` in `compose.prod.yml`. Rate limiting is no longer a blanket gap either — §4.10's demo endpoint has one, since it is the one place that couldn't be left alone, and §4.11's forgot-password endpoint (and `resendVerificationLimiter`, keyed per-account rather than per-IP) needed the exact same treatment for the exact same reason.

**Test suites need to authenticate too.** `smoke.mjs`/`build.mjs` each spawn their own dev/ preview server and a fresh Playwright browser context with no session cookie — with the app now gated behind `AuthGate`, both suites sign in a dedicated `smoke-test@fitmap.local` account via `context.request` (signup, or login if a prior run already claimed that email) before navigating. Since the email-verification gate above would otherwise leave that fixture account stuck on the verify screen with no mailbox to fetch a token from, `SKIP_EMAIL_VERIFICATION` defaults `true` in local dev's `compose.yaml` specifically so this keeps working — a real deployment (`compose.prod.yml`) never sets it. A Docker-only gotcha worth documenting: reaching `api` over service-name DNS (`http://api:8080`) is *cross-site* from the test's own dev server (`http://localhost:5180`) as far as `SameSite=Lax` cookies are concerned — different hostnames, even both being local — so the browser silently drops the session cookie on every request after sign-in, and the suite hangs waiting for a map that never authenticates. `test` uses `network_mode: "service:api"` instead, so both are reachable as `localhost` at different ports only — the same relationship real dev already has between the `web` and `api` services — plus adding the two suites' ports to `corsAllowedOrigins`.

### 4.10 The no-signup demo

**Built.** `VISION.md` §8.2: "a no-signup, drag-a-file-in, see-your-fog-map page" — the single asset that section calls "the single best asset in this plan." Reuses §4.9's entire auth/session/upload/ingest/fog pipeline unchanged, behind a real account, rather than a second, from-scratch client-side parse-and-render path that would mean reimplementing GPX/FIT/TCX parsing and fog rasterization a second time, in a different language, with two implementations to keep in sync forever after.

**`POST /v1/auth/demo` (`handleDemoStart`, auth.go) opens a session against one persistent, shared account** — a fixed `DemoCustomerUserID` constant (`demo_presets.go`), not a fresh row per visitor. Its `demo_expires_at` (`migrations/0017_demo_customer_user.sql`) is a fixed far-future timestamp rather than `NULL`: `isDemo := demoExpiresAt != nil` (auth.go) needs it non-`NULL` to keep the account read-only (`requireNotDemo`) and exempt from email verification (`requireVerified`), while a value that far out never matches the purge sweep's `< NOW()` condition below, so the account is never deleted. `demoSessionTTL` (24h) bounds each visitor's own session cookie — a fresh `POST /v1/auth/demo` call opens another session against the same account, and any number of visitors hold one at once, all seeing identical data. Its `email` (`demo-customer@fitmap.invalid`) is a synthetic, `.invalid`-TLD placeholder; `handleMe` strips it to `""` before it ever reaches the frontend.

**`requireNotDemo` (auth.go) rejects upload, sync, edit, delete, avatar, and settings for a demo session regardless of what a client attempts**; `requireVerified` still passes a demo session through for everything else (list, tiles, the activity graph), since the verification gate was never meant to apply to one. `ActivitiesPanel.tsx`'s `readOnly` prop and `UploadPanel.tsx`'s own (both driven by `MapView.tsx`'s `isDemo`) disable, rather than hide, the corresponding controls — with a `title` explaining why — so a demo visitor sees that editing/deleting/uploading exist rather than wondering. Either way this is a UX courtesy, not the actual enforcement, which is `requireNotDemo` alone.

**The account's history is seeded once, out of band, not per request.** `demo_presets.go`'s `SeedDemoCustomer`, invoked by the `seed-demo-customer` subcommand in `cmd/fitmap/main.go` at deploy time, walks 611 embedded GPX files (`//go:embed demo_data/*.gpx`, about 7 months of a single persona's dog walks, bike rides, local errands, and a few multi-day trips) and ingests each one through the same real `internal/ingest.Process` pipeline a real upload uses. `ON CONFLICT (user_id, source, external_id) DO NOTHING` (§4.1's idempotency invariant) makes re-running the command safe — already-ingested files are skipped, not duplicated, which is what makes a one-time-at-deploy seed workable: re-running it after a redeploy just confirms everything is still there. Seeding per visitor was rejected outright — `handleDemoStart` returning only after 611 sequential parse+DB+fog-render steps would make "Try Demo" take minutes.

**`internal/worker/demo_purge.go`, on a second ticker in `worker.Run`** (5 minutes, far coarser than the job-queue poll), deletes DB rows in batches of 100 via `DELETE FROM users WHERE demo_expires_at < NOW()` — which never matches the shared demo account, by design, since its `demo_expires_at` is fixed in the year 9999. The cascade (every `user_id` foreign key is `ON DELETE CASCADE`) and object-storage sweep (`raw/{userID}/`, `fog/{userID}/`, `heatmap/{userID}/`, via `Store.RemoveByPrefix`) this sweep performs would only ever fire again if a future change reintroduced per-visitor demo rows. **Known, accepted gap**: `activity-masks/{activityID}/` (§4.2.3's per-activity masks) is namespaced by *activity* id, not user id, so these are left orphaned rather than swept — not a correctness bug, just a lower-value cleanup deferred until it measures as worth doing.

**Rate-limited per IP (`demoLimiter`, auth.go)** — 5 session starts per hour, a plain in-memory fixed-window counter with lazy eviction of stale entries, not a library, since this is the one endpoint of the whole "no rate limiting" gap that couldn't be left alone: it's reachable with no session or credentials at all. Keyed by `r.RemoteAddr`'s host, not `X-Forwarded-For` — no reverse proxy sits in front of this server today, so that header would just be an attacker-controlled value.

**Signing up from a demo session is an ordinary new account, not a conversion.** `handleSignup` treats it exactly like a cold signup: a plain new row, gated by email verification like any other. The shared demo account belongs to no one visitor and is untouched either way — there is nothing to carry forward, since nothing done in a demo session was ever that visitor's own data.

**`UserMenu.tsx`'s "Demo session — save this" reuses `AuthGate` itself, full-page, rather than a second form.** "Save this" calls `requestUpgrade()` on `AuthContext`, which `App.tsx` turns into a third top-level render branch — the same `AuthGate`, in signup mode, with an `onCancel` prop that swaps `AuthGate`'s "Try it now — no signup" button for a "← Back" link (starting a second demo mid-demo would just abandon the first) and puts the whole thing back once either `onCancel` fires or signup succeeds.

**`SessionUser` is a real discriminated union, `AuthUser | DemoUser` (`api.ts`), not `AuthUser` plus an `isDemo: boolean` flag.** `DemoUser` is deliberately empty — a demo session carries nothing else worth exposing — which is what lets `'email' in user` narrow a `SessionUser` correctly with no explicit tag needed. The backend's wire format is unaffected (`{email, isDemo}` JSON); the boolean survives only inside `api.ts`'s own parsing, deciding which of the two shapes to construct — `login`/`signup` return a plain `AuthUser` (a successful one is never a demo account) and `startDemo` a plain `DemoUser`.

**Frontend entry point**: `AuthGate.tsx` gains a "Try it now — no signup" button below the sign-in/sign-up form (hidden when `onCancel` is set — see above), calling `api.ts`'s `startDemo()` and then `onAuthenticated` exactly like a real login — `App.tsx` needs no special-casing to treat a demo session as authenticated, since resolving `getCurrentUser()` to a `DemoUser` is already a normal, non-null `SessionUser`.

**Verified live**: `fitmap seed-demo-customer` against a fresh stack ingested all 611 activities (`ingested: 611, already_present: 0, failed: 0`); running it again was a clean idempotent no-op (`ingested: 0, already_present: 611`). `POST /v1/auth/demo` from two separate cookie jars returned distinct session tokens for the same account id, and `GET /v1/activities` returned the identical 611 rows through either session. `POST /v1/activities/upload`/`sync/activities`/`PATCH`/`DELETE` on a demo session all return `403 {"error":"demo_read_only"}`. The purge worker's own query matches zero rows for this account. The 6th demo-start from one address in an hour returns `429`.

Starting a demo session (like any other app-initiated sign-in) clears the URL's saved camera position first — see §4.13's account of why that matters for a shared account with real, unchanging history.

### 4.11 Password recovery

**Built.** The one remaining gap §4.9's own doc comment named from the start: "no password reset (no email-sending infrastructure exists to build either on)." That was the actual blocker — a short-lived opaque token and a new endpoint pair follow patterns this codebase already had (`sessions`, `migrations/0006_sessions.sql`); what's genuinely new is that the app has to send an email for the first time.

**`internal/mail` (new package) — a generic SMTP client, not a vendor's HTTP API**, decided directly: works with any SMTP-speaking service via env vars alone, including a personal Gmail account through an app password (which this app's own real user already has — no new account to provision just to send one kind of email), rather than tying the app to one named transactional-email provider. Stdlib `net/smtp` + `crypto/tls` only — the well-known manual STARTTLS dance net/smtp has no convenience wrapper for, but zero new go.mod dependencies. `SMTP_HOST` unset (compose.yaml's own default) selects a `logSender` instead of a real one — not a stub to replace later so much as a deliberate dev/test mode: the reset flow is fully exercisable, including reading the real emailed link back out of `docker compose logs api`, with no real credentials configured at all. Wiring real `SMTP_*` values into `/.env` afterward is a configuration step for whoever runs this deployment; nothing in code can provision an email account's app password on its own.

**`migrations/0008_password_resets.sql`** mirrors `sessions` exactly: the row's own `gen_random_uuid()` primary key *is* the token, no separate hashing layer, same single-user threat model that decision was already made for. `passwordResetTTL` (1 hour, auth.go) is deliberately much shorter than `sessionTTL`/`demoSessionTTL` — the standard expectation for a reset link, since it's usually emailed somewhere less secure than the session cookie it's standing in for.

**`POST /v1/auth/forgot-password`** always responds `200` with the same generic body whether or not the email matches a real, claimed account — `handleLogin`'s own "don't leak which emails are registered" reasoning, reapplied. Matches only rows with `password_hash IS NOT NULL` (excludes unclaimed placeholder rows and demo accounts, whose email is the internal synthetic one nobody can type in anyway — §4.10). Rate-limited per IP with the same `fixedWindowLimiter` §4.10's `demoLimiter` already introduced (a second instance, `forgotPasswordLimiter` — the type was already generic, so this needed no new code beyond the instance itself): the other endpoint reachable with no credentials at all that has a real-world side effect a script could otherwise abuse — mail-bombing an arbitrary address through this app.

**`POST /v1/auth/reset-password`** looks up the token, generic error if missing or expired (same non-leaking reasoning), and on success: sets the new password, deletes *every* outstanding `password_resets` row for that account (not just the one used — a reset retires every other still-live link too), deletes *every* `sessions` row for that account (a password reset is exactly the moment an already-compromised session should stop working), then starts one fresh session for the browser completing the reset — auto-signed-in, same as signup/login/demo already are.

**Frontend reuses `AuthGate` itself again, adding two more of its internal screens rather than a new page** — the same resolution §4.10 already reached for the demo-upgrade path, applied the same way here. A `screen: 'form' | 'forgot' | 'forgot-sent'` union alongside the existing `mode` toggle: "Forgot password?" (shown only in signin mode) swaps to `'forgot'` (an email-only form calling `forgotPassword`); success swaps to `'forgot-sent'`, a static "if an account exists…" message matching the backend's own non-leaking response — there is nothing more specific for the UI to say either. The reset link itself needs a genuinely new entry point, since it has to work as a cold landing page reached from an email client, before any session check: `AuthGate` gained a `resetToken?: string` prop that, when set, bypasses `mode`/`screen` entirely for a single "set a new password" screen. `App.tsx` reads `?reset_token=` from `window.location.search` once, in a lazy `useState` initializer that also strips it via `history.replaceState` in the same pass (so a refresh mid-form can't re-trigger it, and it doesn't linger in browser history) — checked ahead of every other branch, including an already-live session: arriving via a clicked link is unambiguous intent, outranking whatever `auth` would otherwise resolve to.

**Verified live**, both backend and frontend: requested a reset for the real account, read the logged link, confirmed `docker compose logs api` carries the real token; completed it via curl end to end — old password then fails, new one works, a session cookie captured *before* the reset is dead afterward (checked directly, not inferred), reusing the same token a second time is rejected, an expired token (inserted directly) is rejected; the per-IP `429` fires the same way `demoLimiter`'s already does; a nonexistent email gets the identical response as a real one. Then the full click-through in a live browser: "Forgot password?" → submit → "Check your email" → cold-navigate to the logged link (not a reload — a fresh `page.goto`, matching what actually clicking an emailed link does) → "Set a new password" renders with the token already stripped from the visible URL → submit → redirected into the app, signed in under the reset account's real email.

### 4.12 Account settings (Avatar, Name, Country, Privacy Trim)

`users` carries `display_name`, `country` (`CHAR(2)`, ISO 3166-1 alpha-2), `avatar_key`/`avatar_content_type`/`avatar_updated_at` (`migrations/0001_init.sql`), alongside `privacy_trim_m` (`INT NOT NULL DEFAULT 200`) — this is the first UI that lets anyone change any of them.

**One `authResponse` shape everywhere, not just for `handleMe`.** `loadAuthResponse(ctx, userID)` (auth.go) is now the single place that builds it, and signup, login, demo-start, reset-password, and `handleMe` all call it instead of hand-building a response from just the fields each happened to already have. Deliberate: a naive version would have a fresh signup report `privacy_trim_m: 0` (the Go zero value) rather than the real column default of `200`, looking like every setting had been explicitly cleared rather than never touched. The trade is one extra `SELECT` per auth endpoint, accepted for that consistency.

**`internal/httpapi/account.go` (new)** — `PATCH /v1/account/settings` is a full replace of `display_name`/`country`/`privacy_trim_m` together, not per-field auto-save, matching the Settings page's own single Save button. Validates `country` against `^[A-Z]{2}$` (or empty, to unset) and `privacy_trim_m` against `0–5000` — the frontend already only ever sends a code from a closed dropdown list, so this is a format/range check, not a second copy of the ISO list to validate against.

Avatar is three more endpoints, not folded into the settings PATCH, since it's a different content type entirely: `POST /v1/account/avatar` (multipart, field `"file"`) sniffs the real content type via `http.DetectContentType` against an allowlist (`image/png`, `image/jpeg`, `image/webp`) — **never** trusts the client's claimed extension or declared Content-Type, since that's exactly what `GET /v1/account/avatar` serves the file back as; a mismatch here would let an upload masquerade as an image type it isn't. Stored at a **fixed** per-user key (`avatars/{userID}`, `storage.Store.Put` — the same client §4.0's raw-payload uploads already use), unlike upload.go's content-hashed `raw/{userID}/{hash}{ext}` keys — a re-upload overwrites the previous avatar in place, so there is exactly one blob per account ever, nothing orphaned to clean up. `storage.Store` gained a plain `Remove(ctx, key)` for this (`DELETE /v1/account/avatar`) — the existing `RemoveByPrefix` is built for a list-then-bulk-delete case (a demo account's whole `raw/{userID}/` tree), overkill for a single already-known key. `GET /v1/account/avatar` serves the stored bytes back with the stored `Content-Type` and a week-long `Cache-Control`, made safe by `loadAuthResponse`'s own `avatar_url` carrying a `?v=<avatar_updated_at unix seconds>` cache-busting param — a stale cached copy at the *old* URL is simply never requested again once the avatar actually changes, so aggressive caching costs nothing. Confirmed live: uploaded a PNG, fetched it back with the right `Content-Type`; re-uploaded a second image and confirmed the first was fully replaced, not left behind; uploaded a non-image with a spoofed `.png` filename and confirmed it was rejected (`415`) regardless.

**Frontend: `units.ts` (new) is a hook, not a prop.** `useUnitSystem()` reads `useAuth().user .country` directly and resolves `'metric' | 'imperial'` via a small `IMPERIAL_COUNTRIES` set (`US`, `LR`, `MM` — the three countries where everyday distance is customarily miles/feet). Every component that displays a distance/pace/elevation calls this itself rather than receiving it as a prop threaded down from `MapView`/`ProfilePage` — avoids prop-drilling through chains like `MapView → ActivitiesPanel → DistanceFilter` for a value that only ever changes when Settings saves a new Country.

**`format.ts`'s distance/pace/elevation formatters were rewritten to own their own unit suffix**, not have one appended by every caller: `formatDistanceKm` → `formatDistance`, `formatTotalKm` → `formatTotalDistance`, `formatTotalElevation` → `formatElevation`, each now taking a `UnitSystem` and returning the full string ("12.3 km" / "7.6 mi"), never a bare number a caller then appends a hardcoded `"km"`/`"m"` to. `formatPace` converts the pace value itself, not just its label (seconds per mile via `1609.344 / metersPerSecond`, not just relabeling a per-km number). This mattered beyond tidiness: that exact hardcoded-suffix pattern is what let `useMapInstance.ts`'s own `ScaleControl` drift to a hardcoded, unexplained `unit: 'imperial'` — inconsistent with every km display elsewhere in the app — for as long as it did, unnoticed, since nothing forced the two to agree. Every call site across `ActivitiesPanel.tsx`, `DistanceFilter.tsx` (then still `ActivityFilters.tsx`), `RangePicker.tsx`, `YearGrid.tsx`, `UploadPanel.tsx`, `ActivityGraph.tsx`, `Trends.tsx`, and `TrackProfile.tsx` was updated to call `useUnitSystem()` and pass it through.

**The map's own `ScaleControl` now tracks Country live, via MapLibre's own `setUnit`, not a rebuilt control.** `useMapInstance.ts` keeps the control instance in a ref, seeds it from a new `scaleUnit` option at construction, and a second effect (deliberately separate from the mount effect, which only ever runs once per map instance) calls `scaleControlRef.current?.setUnit(scaleUnit)` whenever it changes — `MapView.tsx` passes `useUnitSystem()`'s value through. Confirmed live: saved Country as `US`, watched the scale bar switch to feet/miles with no reload; switched to `DE`, watched it switch back to meters/ km, still with no reload.

**Verified live, end to end**: signed up, opened Settings from the account menu, uploaded an avatar (confirmed it appears immediately in the account-menu button, everywhere, with no reload), set Name and Country (`US`) and a new Privacy Trim value, saved — confirmed the Activities panel's row distances, its footer's "loaded"/selected-group summaries, the distance-filter slider, and the Profile page's stat cards and Trends tooltip all switched to miles/feet immediately. Also confirmed no console errors across the whole flow.

**Profile and Settings cross-link to each other, not just back to the map.** Each passes the *other* screen's open handler through: `ProfilePage` takes `onOpenSettings` and passes it to `Header`; `SettingsPage` takes `onOpenProfile` the same way; `App.tsx` wires both from the same `setView`. Each screen omits the prop that would link to *itself* (`ProfilePage` never gets `onOpenProfile`, `SettingsPage` never gets `onOpenSettings`) — there's nowhere further to open a screen to when it's already open (`Header.tsx`'s own doc comment).

### 4.13 Map opening view and camera position (FR-4.5)

`MapView.tsx` resolves the opening camera in a fixed order, each step a separate one-shot effect guarded by its own ref: a saved/shared URL position, then the account's single most-recent activity, then its Country setting, then a fixed world view (`WORLD_VIEW`, `config.ts`) — never the browser's geolocation permission, which stays scoped to FR-4.7's separate, user-clicked "Find my location" control (`useMapInstance.ts`'s `GeolocateControl`).

**The URL hash is read once per mount, not once per page load.** `parseHash(window.location.hash)` used to run as a module-level `const`, evaluated exactly once when the page's JS first loaded — remounting `MapView` for a new session (sign-out then sign-in, or starting a demo session, all in the same tab) did not re-run it. Combined with nothing ever clearing the hash on an identity change, a fresh session could inherit a previous session's last camera position and silently skip its own fly-to-most-recent-activity step, since FR-4.5's "a saved URL position wins" rule was working exactly as designed against stale data (reported live against the Demo Customer account: it stopped flying to its own history after a same-tab sign-out/demo-start cycle, and started working again only after a full page reload — which is exactly what re-evaluated the old module-level read). Fixed two ways together, since either alone is insufficient: `MapView.tsx` now reads the hash into a `useState(() => ...)` lazy initializer (per-mount, not per-page-load), and `viewState.ts`'s `clearSavedView()` is called at every app-initiated identity change in `App.tsx` (sign-in, sign-up, demo-start, sign-out — all funneled through one `handleAuthenticated`/`handleSignOut` pair) — never on the mount-time session check, where an existing hash is a legitimate same-session "return to where I was" on a plain reload.

**The zero-history fallback effect uses `useActivityDays()`'s `earliest`/`ready`, not `activities.length === 0`, to detect "genuinely no history."** `activities` is scoped to `selectedRange`, which degenerates to `{today, today}` for a brand-new account regardless of whether it actually has any history at all (§4.7's own note on the same trap for the default-range effect) — `earliest` stays permanently `null` only for an account with no activities ever, which is the real signal this fallback needs.

**`countryView.ts` (new)** is a hand-authored `Record<ISO 3166-1 alpha-2, ViewState>`, generated once, offline, from the same Admin-0 country polygons already seeded server-side for Fog/Heatmap's country unlocking (`admin_countries`, §4.4): `ST_PointOnSurface(geom)` per `iso_a2` (not `ST_Centroid`, which can land outside a concave or archipelago shape) for the point, and a zoom derived from the polygon's own bounding-box extent so a small country lands close and a large one lands wide. A handful of countries needed a hand override rather than the derived value: every antimeridian-crossing country (Russia, the United States, Fiji, Kiribati, New Zealand, Antarctica) breaks the bbox-extent calculation outright at the ±180° seam, and three more (France, the Netherlands, Norway) bundle a far-flung overseas dependency into the same `admin_countries` polygon as the mainland — France and the Netherlands still centroid correctly onto the mainland but with a wildly oversized derived zoom, while Norway's centroid lands on Svalbard instead. Scoped to exactly the codes `apps/web/src/ui/countries.ts`'s `COUNTRIES` list can write into `users.country`; a handful of small territories in that list have no polygon in `admin_countries` and simply have no entry, falling through to `WORLD_VIEW` like an unset country does. `countryView(country)` mirrors `units.ts`'s `unitSystemForCountry` null-handling convention (`''` or an unmapped code both resolve to `null`).

**`WORLD_VIEW` (`config.ts`) replaced `DEFAULT_VIEW`**, a hardcoded Columbus, OH point that predated this fallback chain entirely and had no design rationale behind it beyond "roughly the centre of the [dev] extract" — it served as the no-hash opening view before fly-to-most-recent-activity existed, and remained the silent, undocumented result of every zero-history load afterward, with no fallback logic at all. `WORLD_VIEW` (`{ longitude: 10, latitude: 15, zoom: 1.3 }`) is deliberately zoomed out far enough to keep every continent in frame, and is now the one fallback used both by the resolution chain's last tier and by `MapView`'s own no-hash-at-all default.

**Signed-out browsing is unaffected** — there is no `MapView` mount without an active session at all today (`App.tsx`'s signed-out branch renders only `AuthGate`), so this resolution chain, and the "no login wall" framing `docs/ROADMAP.md` aspires to for web, don't yet apply there; see `docs/ROADMAP.md`'s own item tracking that gap.

---

## 5. Engineering Risks & Mitigations

### 5.1 Ingest path risk, per path

Three paths means three different failure modes, and the mitigation is that they are independent.

**Path 1 — provider terms and licences.** A business risk before an engineering one; see `VISION.md` §4.1. Engineering consequences:
* **Never poll.** Webhook receivers only. Return `200 OK` in milliseconds and enqueue.
* **Rate-limit defensively.** A token-bucket limiter per provider, shared across workers. Backfill is the dangerous operation — thousands of activities on connect.
* **Deletion on deauthorization.** Required by all three activity providers. Build it with the first connector, not after.
* **Assume revocation.** Everything must work with zero connected providers.

**Path 2 — platform route access.** Verified constraints, not assumptions: Android route reads are foreground-only and cannot be requested programmatically; Samsung Health exposes no route geometry at all. **Degrade honestly** — a session with no route must be visibly marked "no route" rather than silently contributing nothing to the map. The failure mode to avoid is a user syncing 400 activities and seeing an empty map with no explanation.

**Path 3 — malformed input.** The unconditional path is also the one fed arbitrary user files. Assume hostile input: bound decompression of archives (zip bombs), cap file size and point count, reject non-conforming FIT gracefully, and never let one bad file in a 5,000-file bulk import abort the batch.

### 5.2 Parsing overhead

* Stream `.FIT` records; never buffer the whole file.
* Bound worker concurrency by available memory, not CPU count — the constraint is the point buffer, not the decode.
* Cap raw points per activity with a sane ceiling and downsample beyond it.
* **Bulk imports are the stress case.** A Strava export archive is thousands of activities arriving at once. Process as individually-queued jobs with per-user fairness, so one user's 10-year backfill cannot starve everyone else's uploads.

### 5.3 Tile payload at low zoom

Solved by construction rather than by rollup jobs. Fog uses a downsampled raster pyramid, so a z3 fog tile is the same handful of KB as a z14 one. Track MVTs stay bounded through zoom-dependent simplification, and below z8 tracks are hidden entirely (`CITY_MIN_ZOOM`, `zoomTiers.ts`) — at that scale the fog mask *is* the picture, and below z8 the fog/heatmap raster itself gives way to the Country/Region tiers (§4.2.4), which need no per-tile payload concern at all: each tile is a handful of whole-polygon fills from a fixed ~250/~4,600-row dataset, not simplified per-user geometry.

### 5.4 Basemap cost and build

Metered tile providers charge per request, and an exploration product exists to provoke pan and zoom. Self-host Protomaps from the start.

**The extract is planet-wide.** A regional extract means every user outside the region sees a blank map. The full planet build is **~138 GB** (the 2026-09-10 build is 137,906,248,094 bytes, version 4.15.2). At R2 storage pricing that is a couple of dollars a month with no egress charge, which retires the coverage problem permanently rather than managing it.

Protomaps publishes daily planet builds at `https://build.protomaps.com/{YYYYMMDD}.pmtiles`, indexed at `https://build-metadata.protomaps.dev/builds.json`. **Pin a dated build key rather than "latest"; the bucket retains roughly the past week**, so a pinned key ages out and must be refreshed deliberately.

**Serving.** PMTiles requires HTTP range-request support plus CORS — allowed methods `GET, HEAD`, allowed headers `range, if-match`, exposed header `etag`. With those set the client reads the archive directly; no proxy or tile server is needed. Note that **every tile read is a separately billed GET** — that, not storage, is the cost that scales, and it is why a CDN sits in front.

**Rollout.** Publish updates under content-addressed names (`planet-20260910.pmtiles`) rather than overwriting in place, so in-flight clients are never served a half-swapped archive.

**Retired by going planet-wide:** the client-side check of track bounds against archive bounds and the "this area isn't in the demo basemap" message, in `apps/web/src/map/coverage.ts` and `apps/web/src/ui/CoverageNotice.tsx`. Remove them rather than leaving a message that can no longer fire.

### 5.5 Export rendering

**The first export slice (a high-resolution map image, §4.3.3) is client-side, not a server-side render.** See `docs/adr/0005-client-side-export-rendering.md` for the full decision — why, the alternatives considered, and what it costs versus what it buys; this section covers the mechanism, not the reasoning. It works by pointing a second, temporary, hidden `Map` instance at a larger container and capturing its canvas — see `apps/web/src/map/exportMap.ts`'s own doc comment for the full mechanism. Because it costs the server nothing, there's also nothing to rate-limit here the way the paragraph below still correctly anticipates for a server-side render.

This client-side path has a real ceiling worth naming honestly: it can only export what the browser itself can already render at whatever resolution its own canvas will allocate, which is generously larger than a screenshot but not a calibrated print DPI against a known physical output size, and it can't outrun what a single browser tab's GPU/memory can hold for one temporary map instance. If that ceiling turns out to matter — a specific print product, or output large enough that a client-side canvas becomes unreliable — the original plan below is still the right design for that case, not a discarded idea:

Exports are free and unwatermarked, so they will be used more than a paid version would be — budget for that rather than treating them as rare. Render in the worker pool, not the API process, and deliver asynchronously. Print-grade output needs a genuinely higher-resolution path than the screen renderer: a headless MapLibre render at print DPI, driven by the shared style document from §2.1, not an upscaled screenshot.

Rate-limit export generation per user. It is the most expensive thing an individual can trigger on demand, and there is no payment step in front of it.

### 5.6 Attribution

The Protomaps basemap is an ODbL "Produced Work" and OSM attribution is mandatory and must be visible on the map. This applies to **exports as well as the screen** — an exported image is a distributed Produced Work. The bundled Noto Sans glyphs are SIL Open Font License; ship `OFL.txt` alongside them.

```
<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org">OpenStreetMap</a>
```

MapLibre Native does not render an attribution control by default in every configuration, so this is verified on each mobile client rather than assumed. On Android it is: `MapView` shows the attribution control on its own, and it picks up the credit the style document's `protomaps` source already carries — confirmed on a physical device against the served style (`apps/android/docs/ROADMAP.md`, Phase 2), so the Android app adds no attribution code of its own and must not disable the control.

### 5.7 Cost control — an engineering requirement, not an ops concern

New, and specific to being free (`VISION.md` §4.3, §6.3). Costs scale with users; donations scale with goodwill. Nothing reconciles those two curves automatically, so the system has to bound itself.

* **Retention and dormancy.** `activity_streams` is the largest storage line and the least frequently read. Tier it: after N months of account inactivity (`users.last_seen_at`), move streams to cold storage or drop them, keeping `activities` summaries and fog rasters so the map still renders. Warn by email first, and make it recoverable by re-upload. This is the single most effective lever on the cost curve.
* **Raw payload retention.** Raw ingest payloads (Path 3 originals, Path 1 provider pushes, Path 2 synced point batches) in object storage are not pure insurance — they are what a `reprivacy` job re-parses from (§7), since `activity_streams` never carries position. Expire them on a schedule anyway; they are also the most sensitive artifact we hold (§7). An activity whose payload has already expired simply cannot be retroactively re-clipped from source — that is an accepted limit of the retention window, not a bug.
* **Per-user quotas.** A generous but finite cap on activities and total points. Not to monetise — to prevent one pathological account from becoming a material share of the bill.
* **Rate limits everywhere, with a spend cap.** Uploads, exports, tile requests and provider backfills. A CDN and object-store spend cap is load-bearing infrastructure: a free product has no natural throttle and a front-page day is a cost event with no matching revenue event.
* **Measure cost per active user from day one.** It is the number that decides whether `VISION.md` §6's funding model works, and it cannot be reconstructed retroactively.

### 5.8 Minimal deployment topology

**Built — repo scaffolding only, not an actual running deployment.** `docs/DEPLOY.md` is the runbook; this is the architecture behind it. One small VPS, four containers via `compose.prod.yml` (a standalone file, not a `compose.yaml` overlay — different-enough topology that layering edits on the dev file would be more confusing than a clean second one): Postgres+PostGIS, `api`, `worker`, and Caddy in front of the built static frontend. Object storage is real Cloudflare R2 (§4.3's own reasoning for choosing it — no egress fee) reached over the public internet, so there's no `minio` service here the way dev's stack has one.

**One container does both reverse-proxying and static serving.** See `docs/adr/0006-minimal-deployment-same-origin-caddy.md` for why (Caddy vs. nginx, one origin vs. two). Mechanically: Caddy (`apps/web/docker/Caddyfile`) terminates TLS (automatic Let's Encrypt, zero manual certbot/cron setup), serves the built frontend, and reverse-proxies `/v1/*`, `/tiles/*`, and `/healthz` to `api` — all from **one origin**. That single-origin choice is what lets `apps/web/Dockerfile`'s production build stage set `VITE_API_BASE_URL=""` unconditionally (a relative API base, resolved against whatever domain it's actually served from) and is why `httpapi/server.go`'s `corsAllowedOrigins` needs no production entry added at all: a same-origin fetch is never a cross-origin request. Only reconsider `corsAllowedOrigins` if the frontend and API are ever deliberately split onto different origins.

**The session cookie's `Secure` flag is derived, not hardcoded.** `auth.go`'s `startSession` reads `strings.HasPrefix(s.appBaseURL, "https://")` — reusing `APP_BASE_URL` (already the one config value that names the frontend's real origin, for password-reset links) rather than introducing a second env var that could disagree with it. Dev's default (`http://localhost:5173`) keeps this `false`, unchanged; `docs/DEPLOY.md` setting a real `https://` `APP_BASE_URL` is what flips it on.

**The basemap archive can never be baked into the production image.** `apps/web/.dockerignore` deliberately excludes every `*.pmtiles` file from *every* build context ("the container reads it through the bind mount at runtime, never from the image" — true for dev's bind mount, and it means a from-scratch production build has no local archive to fall back to either). New `config.ts` function `basemapOrigin()` — reads a build-time `VITE_BASEMAP_ORIGIN` (baked in via a Docker build ARG threaded through `apps/web/Dockerfile`'s `build` stage and `compose.prod.yml`'s `web` service), falling back to `browserOrigin()` when unset — is what `useMapInstance.ts`/`exportMap.ts` now pass to `buildStyle` instead of calling `browserOrigin()` directly; `style.ts` itself needed no change, since `buildStyle`'s `origin` parameter already accepted an arbitrary absolute origin by design. `docs/DEPLOY.md` documents two ways to actually supply the archive: bind-mount the real extract into the `web` container (simplest, `VITE_BASEMAP_ORIGIN` left unset, no rebuild to update it) or host it on R2/a CDN and set `VITE_BASEMAP_ORIGIN` to that origin (matches this section's own R2 reasoning; worth doing once tile-read volume justifies a CDN in front of it, per §5.3/§5.7's own cost-control reasoning) — start with the former.

**Verified directly**, not just written: `apps/web/Dockerfile`'s new `build`/`serve` stages build successfully end to end (`tsc --noEmit && vite build`, then the Caddy image copying the result); `caddy validate` against the built image's own `/etc/caddy/Caddyfile` reports a valid configuration once `DOMAIN` is set (and — confirmed directly — fails a different, expected way when it isn't, since Caddy then parses the site block as the global options block instead); `docker compose -f compose.prod.yml config` resolves cleanly against a test env file; the existing dev stack's own `verify:map`/`verify:build` suites still pass unchanged after `basemapOrigin()`'s introduction, and a live signup against the running dev `api` confirms the session cookie still carries no `Secure` attribute under dev's plain-HTTP `APP_BASE_URL`, exactly as before this change.

### 5.9 Mobile browser support

**Built, but not actually usable yet.** The layout decisions below exist in code and were deliberately designed, not guessed at — but the real mobile experience has been reported directly as unusable, not merely rough, so this needs rework (real-device testing, not just CSS review) before it can be called done. No mobile mockup ever existed for this — this section, not a design doc, was meant to be the record of the actual layout decisions, and still is for whoever picks this back up. Investigated directly before building: zero `@media` queries existed anywhere in `index.css`; the Activities panel was a permanent, fixed-width (260–560px, drag-resizable) sidebar; roughly seven interaction sites were hover-only with no touch equivalent; `RangePicker.tsx`'s drag handles were 14px wide (`touch-action: none` was already set, so mechanically draggable, just tight for a fingertip); `Header.tsx`'s Donate/Upload/Export controls were plain text buttons with no icon fallback for a narrow screen. **Decided directly with the user, not assumed:** the Activities panel becomes a collapsible bottom sheet (not a separate Map/List tab screen), and scope is "core flows fully touch-usable," not full parity with every hover-dependent FR — `TrackProfile.tsx`'s colored-band/elevation values (FR-4.9) and the map-track-hover ↔ Activities-row-underline highlight (FR-4.1/FR-5.4) stay mouse-only, documented in `SPEC.md` §13 as a deliberate boundary, not a gap discovered later.

**One new `@media (max-width: 768px)` layer**, isolated at the end of `index.css` — every rule above it is the desktop layout, unmodified and confirmed unaffected (the existing desktop-viewport `verify:map`/`verify:build` suites were re-run unmodified specifically to prove this, not just assumed from the media query's isolation).

**The Activities panel becomes an overlay, not a layout-affecting sidebar, under the breakpoint** — `position: fixed`, which removes it from `.app-body`'s flex row entirely, so `.map-root` (the only flex child left, `flex: 1`) naturally expands to fill the width with no `.app-body` change needed at all. Deliberate: since the map's own container dimensions never change with the sheet open or closed — only what's drawn on top of it does — no `map.resize()` call is ever needed, sidestepping the resize-thrashing jank a width/height-affecting panel would risk during a drag. `ActivitiesPanel.tsx` gained a local `sheetExpanded` boolean (same shape as its own existing `filtersOpen`), collapsed by default; the tap target is the panel's own existing count/distance subtext line, converted from a `<p>` to a `<button>` reset to look identical to plain text at desktop width (`cursor: default` there — only the mobile query gives it `cursor: pointer` — so clicking it at desktop width is an invisible no-op, not a behavior change). The width-drag resize handle is hidden via CSS alone under the breakpoint, no JS branching needed.

`panelWidth` moved from a direct inline `style.width` to a CSS custom property (`style={{ '--panel-width': ... }}`, consumed by `.activities-panel { width: var(--panel-width); }`) specifically so the mobile media query's own `width: 100%` rule can cleanly override it — an inline style always wins over an external stylesheet rule short of `!important`, which this codebase otherwise never needs; a custom property consumed by a normal declaration doesn't have that problem.

**Two gotchas worth not rediscovering, since both generalize beyond this one feature:**

1. **The histogram/date-range-picker footer (`ActivityHistogram.tsx`/`RangePicker.tsx`) needs `flex-wrap` on its header row.** The header row (the range summary on the left, the pan window label and Earlier/Later buttons on the right) has no shrink/wrap behavior at desktop widths, but on a ~390px phone the two sides together are wider than the screen — which doesn't just clip locally, it forces the *whole page* wider than the viewport, which then cascades into the Activities panel sheet's own `width: 100%` resolving against that widened layout viewport instead of the real one, corrupting its layout too. Fixed at the source: `.activity-histogram__header { flex-wrap: wrap; }`, plus a little extra height on `.activity-histogram` itself to fit the wrapped second line without stealing space from `.range-picker__chart` (`flex: 1` below it) — not by patching the symptom on the sheet. A defensive `overflow-x: hidden` on `html`/`body` is a backstop for the small remainder (a few px from the range-picker handles' own deliberately widened hit area extending past their logical box). (The footer's header-row layout this fix targeted no longer exists — §4.7's legend-column-plus-chart-area redesign replaced it and needed a related but distinct mobile fix of its own: the taller stacked footer's chart area landed in the same bottom 68px of the viewport the collapsed Activities sheet always occupies, `position: fixed` and all, hiding the Earlier/Later buttons under it entirely rather than merely sitting beside them. Fixed by giving `.activity-histogram` a `padding-bottom: 68px` in the mobile query, reserving that space the same way `.maplibregl-ctrl-bottom-left/-right`'s own `bottom: 78px` already reserves space for controls positioned *inside* the map — the generalizable lesson from this gotcha, not the specific selector, is what carried forward.)
2. **A tap's browser-synthesized compatibility mouse events broke the very tap fallback they were supposed to coexist with.** `Trends.tsx`'s tap-to-show fallback (§5's own design: an `onClick` alongside the existing `onMouseEnter`/`onMouseLeave`) worked in isolation but failed intermittently once exercised after other page interactions — a touchscreen tap still triggers a full compatibility mouse-event sequence (for the benefit of any site only listening for mouse events), `mouseleave` included, *after* the click — which immediately re-cleared the exact state `onClick` had just set, silently defeating the fallback. Fixed by switching from `onMouseEnter`/`onMouseLeave` to `onPointerEnter`/`onPointerLeave`, guarded by `event.pointerType === 'mouse'` — `PointerEvent` (unlike `MouseEvent`) carries which input actually produced it, so a real mouse hover still works exactly as before and a touch-synthesized one is correctly ignored, leaving `onClick` as the sole, unconflicting source of truth for touch.

**Verified live**, phone-emulated (Playwright's `devices['iPhone 13']` profile, 390×844, `hasTouch: true`): no horizontal overflow anywhere (header, body, Settings page); the Activities panel starts collapsed (~68px) and the map pans correctly while it's collapsed; tapping the sheet toggle expands it (~520px+) and tapping again collapses it back; the resize handle is confirmed hidden; the range-picker handle's computed width is the widened 30px, not 14px; `Trends` tooltips appear on a tap and clear on tapping elsewhere in the chart; zero console errors across the whole run. Desktop's own `verify:map`/`verify:build` suites both still pass unmodified afterward.

---

## 7. Privacy & Security Implementation

Privacy is enforced at **ingest**, not at render. Once a point is excluded at step 3 of §4.1, no downstream bug can leak it, and no cached raster or exported image can contain it.

* **Privacy zones** — user-defined circles; points inside are dropped and the trajectory is split rather than bridged across the gap.
* **Endpoint trimming** — `privacy_trim_m`, default 200 m, applied to both ends of every track, opt-out rather than opt-in.
* **Retroactive application** — adding a zone must re-process existing activities and mark affected fog tiles dirty (`jobs.kind = 'reprivacy'`). A `reprivacy` job re-parses the activity's stored raw payload (`activities.raw_payload_key`) and re-runs ingestion steps 2–6 of §4.1 against the new zone set — the same code path as initial ingest, not a bespoke re-clip. Build this with the feature; it is the case users actually hit. This is also the reason privacy is applied server-side rather than before upload: retroactive re-clipping requires the server to hold re-clippable raw points, which is why raw payloads are retained per source (§5.7) rather than discarded after parse.
* **Raw payload handling** — payloads from all three paths retain unclipped data, including points inside privacy zones, and are what retroactive re-clipping re-parses from (above). They must be private, server-side-encrypted, never publicly addressable, and aggressively expired (§5.7). This is the most sensitive artifact in the system.
* **Provider tokens** — encrypted at rest with a key outside the database, never logged, never returned by an API.
* **Deauthorization deletion** — disconnecting a provider must delete the data synced from it, not merely the `connections` row. Required by Garmin, Wahoo and COROS.
* **Special-category data** — location plus health data is GDPR Art. 9 data. DPIA before launch, working export and deletion endpoints, documented retention, EU-region storage for EU users. **Being free changes none of this.**
* **Tile authorization** — `/tiles/…?user_id=` is an IDOR. Derive the user from the session; never accept a user ID as a tile parameter. Signed, expiring URLs for any shared map.
* **The no-signup demo does hold data, deliberately — bounded by a TTL, not by refusing to persist.** See `docs/adr/0004-demo-account-reuses-real-pipeline.md` for why, the alternatives considered, and the tradeoff. The privacy properties that actually matter still hold — ingest-time trimming/privacy-zone enforcement (above) applies identically to a demo account, and `internal/worker/demo_purge.go`'s sweep is what keeps it from becoming a permanent, anonymous storage tier: `demo_expires_at` bounds the row's life to 24h, and the purge deletes both the DB rows (cascaded) and the object-storage keys (`raw/{userID}/`, `fog/{userID}/`, `heatmap/{userID}/`). "Must not become an anonymous upload endpoint" is enforced by that TTL and by `demoLimiter`'s per-IP rate limit (§4.10), not by refusing to accept uploads at all.