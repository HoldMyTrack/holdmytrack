-- The country and region outlines `seed-admin-boundaries` loads (docs/IMPLEMENTATION.md §4.2.4,
-- ADR-0034), cut from one pinned Overture Maps release's divisions theme. Run by
-- scripts/boundaries-extract.sh, which sets getvariable('release') and getvariable('out').
--
-- Overture's divisions are OpenStreetMap's boundaries (plus a few CC BY 4.0 sources) under ODbL,
-- so this file is also the public record of how the derived outlines were made.
INSTALL spatial;
LOAD spatial;
INSTALL httpfs;
LOAD httpfs;
SET s3_region = 'us-west-2';

CREATE TABLE area AS
SELECT division_id,
       subtype,
       country,
       region,
       COALESCE(names.common['en'], names."primary") AS name,
       geometry
FROM read_parquet('s3://overturemaps-us-west-2/release/' || getvariable('release') ||
                  '/theme=divisions/type=division_area/*', hive_partitioning = 1)
-- The land-clipped outline: a coastal country's other outline runs 12 nautical miles out to sea.
WHERE subtype IN ('country', 'dependency', 'region') AND is_land;

-- A region with no ISO 3166-2 code that lies mostly inside one that has one is the other side of
-- a dispute drawn twice (Aksai Chin, Western Sahara, South Tibet), not a region of its own.
-- Code-less regions that are, like Puerto Rico's municipalities, stay.
CREATE TABLE duplicate AS
SELECT DISTINCT n.division_id
FROM area n
JOIN area k ON k.subtype = 'region' AND k.region IS NOT NULL AND ST_Intersects(n.geometry, k.geometry)
WHERE n.subtype = 'region' AND n.region IS NULL
  AND ST_Area(ST_Intersection(n.geometry, k.geometry)) > 0.5 * ST_Area(n.geometry);

-- One row per outline, in the column order seed-admin-boundaries COPYs into its staging table:
-- kind, key (a country's code, a region's Overture division id), country code, region code,
-- name, geometry as hex WKB.
COPY (
    SELECT 'country' AS kind, country AS key, country, NULL AS code, name, ST_AsHEXWKB(geometry) AS geom
    FROM area WHERE subtype IN ('country', 'dependency')
    UNION ALL
    SELECT 'region', division_id, country, region, name, ST_AsHEXWKB(geometry)
    FROM area WHERE subtype = 'region' AND division_id NOT IN (SELECT division_id FROM duplicate)
    ORDER BY 1, 2
) TO (getvariable('out')) (FORMAT csv, HEADER false, COMPRESSION gzip);
