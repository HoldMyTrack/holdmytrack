-- Country/state-region boundary polygons backing the coarse zoom tiers Fog and Heatmap fall
-- back to below city zoom (internal/geo, docs/IMPLEMENTATION.md §4.2.4): "have you been
-- anywhere in this country/region at all," answered against these polygons rather than the
-- per-pixel raster pyramid, which stays unchanged above that threshold.
--
-- adm0_a3/adm1_code (not iso_a2/iso_3166_2) are the reseed idempotency keys: Natural Earth's
-- own ISO columns are not reliably unique (a few small dependencies share their parent
-- country's ISO code, e.g. Australia's Indian Ocean/Ashmore & Cartier territories both carry
-- "AU"), while adm0_a3/adm1_code are Natural Earth's own stable per-feature codes.
CREATE TABLE admin_countries (
    id       SERIAL PRIMARY KEY,
    adm0_a3  TEXT NOT NULL UNIQUE,
    iso_a2   CHAR(2), -- informational only; nullable and intentionally not unique, never joined on
    name     TEXT NOT NULL,
    geom     GEOMETRY(MultiPolygon, 4326) NOT NULL -- 4326 to match activities.trajectory directly
);
CREATE INDEX idx_admin_countries_geom ON admin_countries USING GIST (geom);

-- Every admin_countries row has at least one admin_regions row (confirmed against the actual
-- Natural Earth 1:10m Admin-1 dataset: even single-region sovereign states like Monaco or
-- Vatican City carry their own one-region entry) — no synthetic "whole country as one region"
-- rows are needed here.
CREATE TABLE admin_regions (
    id         SERIAL PRIMARY KEY,
    country_id INT NOT NULL REFERENCES admin_countries(id),
    adm1_code  TEXT NOT NULL UNIQUE,
    code       TEXT, -- ISO 3166-2 where Natural Earth has one; NULL otherwise
    name       TEXT NOT NULL,
    geom       GEOMETRY(MultiPolygon, 4326) NOT NULL
);
CREATE INDEX idx_admin_regions_geom ON admin_regions USING GIST (geom);
CREATE INDEX idx_admin_regions_country ON admin_regions (country_id);

-- One row per (activity, country/region) it touches, computed once — internal/geo.Match, called
-- from ingest.Process right after an activity is persisted, and backfilled once for existing
-- activities by the `seed-admin-boundaries` subcommand. The country/region tile queries
-- (internal/httpapi's admin_country_tiles.go/admin_region_tiles.go) then only ever do a cheap
-- indexed EXISTS/NOT EXISTS lookup against these — the ST_Intersects test itself never runs at
-- request time.
--
-- ON DELETE CASCADE on activity_id means handleDeleteActivity needs no new code: a deleted
-- activity's membership rows disappear with it, and the country/region re-locks on the very
-- next tile request since "unlocked" is a live query, not a cached/invalidated one.
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
