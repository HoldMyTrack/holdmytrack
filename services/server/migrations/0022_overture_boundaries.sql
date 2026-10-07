-- IMPLEMENTATION.md §3.12-§3.15, §4.2.4, ADR-0034: country and region outlines from Overture
-- Maps' divisions (OpenStreetMap's borders) in place of Natural Earth's.
--
-- An outline is now kept twice. admin_countries.geom/admin_regions.geom are simplified, for the
-- Country/Region tiles drawn from them on every request. The full-detail outline lives only in
-- admin_country_parts/admin_region_parts, cut into small pieces (ST_Subdivide) so matching an
-- activity's track against them stays an index lookup however long the coastline.
--
-- The rows themselves are replaced by `seed-admin-boundaries`. Until it runs, the Natural Earth
-- rows stay, renamed: their own outlines, made valid, become their parts, so matching works in
-- between.
ALTER TABLE admin_countries RENAME COLUMN adm0_a3 TO code; -- ISO 3166-1 alpha-2 (Overture's own X- codes for disputed areas)
ALTER TABLE admin_countries DROP COLUMN iso_a2;
ALTER TABLE admin_regions RENAME COLUMN adm1_code TO overture_id; -- Overture's division id: not every region has an ISO 3166-2 code

CREATE TABLE admin_country_parts (
    country_id INT NOT NULL REFERENCES admin_countries(id) ON DELETE CASCADE,
    geom       GEOMETRY(Polygon, 4326) NOT NULL
);
CREATE INDEX idx_admin_country_parts_geom ON admin_country_parts USING GIST (geom);
CREATE INDEX idx_admin_country_parts_country ON admin_country_parts (country_id);

CREATE TABLE admin_region_parts (
    region_id INT NOT NULL REFERENCES admin_regions(id) ON DELETE CASCADE,
    geom      GEOMETRY(Polygon, 4326) NOT NULL
);
CREATE INDEX idx_admin_region_parts_geom ON admin_region_parts USING GIST (geom);
CREATE INDEX idx_admin_region_parts_region ON admin_region_parts (region_id);

INSERT INTO admin_country_parts (country_id, geom) SELECT id, (ST_Dump(ST_Subdivide(ST_CollectionExtract(ST_MakeValid(geom), 3), 256))).geom FROM admin_countries;
INSERT INTO admin_region_parts (region_id, geom) SELECT id, (ST_Dump(ST_Subdivide(ST_CollectionExtract(ST_MakeValid(geom), 3), 256))).geom FROM admin_regions;

-- Which extract the outlines came from, so re-running the seed with the same one skips the
-- reload and the re-match of every activity.
CREATE TABLE admin_boundaries_source (
    only_row  BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (only_row),
    name      TEXT NOT NULL,
    loaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
