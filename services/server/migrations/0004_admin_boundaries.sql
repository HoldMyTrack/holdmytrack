-- Country/region boundaries behind the coarse zoom tiers Fog and Heatmap fall back to below
-- city zoom (IMPLEMENTATION.md §3.12-§3.15, §4.2.4). The polygons are filled by the
-- `seed-admin-boundaries` subcommand, not here.
--
-- adm0_a3/adm1_code (not iso_a2/iso_3166_2) are the reseed idempotency keys: Natural Earth's
-- own ISO columns are not reliably unique (a few small dependencies share their parent
-- country's ISO code), while adm0_a3/adm1_code are its stable per-feature codes.

-- §3.12 admin_countries
CREATE TABLE admin_countries (
    id       SERIAL PRIMARY KEY,
    adm0_a3  TEXT NOT NULL UNIQUE,
    iso_a2   CHAR(2), -- informational only; nullable and intentionally not unique, never joined on
    name     TEXT NOT NULL,
    geom     GEOMETRY(MultiPolygon, 4326) NOT NULL -- 4326 to match activities.trajectory directly
);
CREATE INDEX idx_admin_countries_geom ON admin_countries USING GIST (geom);

-- §3.13 admin_regions. Every country has at least one region in Natural Earth's 1:10m Admin-1
-- data (even Monaco), so no synthetic "whole country as one region" rows are needed.
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

-- §3.14/§3.15 -- one row per (activity, country/region) it touches, computed once by
-- internal/geo.Match right after ingest persists an activity, so the tile queries only ever do an
-- indexed EXISTS lookup. ON DELETE CASCADE: a deleted activity's rows go with it, and the
-- country/region re-locks on the next tile request.
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
