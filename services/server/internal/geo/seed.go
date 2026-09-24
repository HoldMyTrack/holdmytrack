package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedAdminBoundaries loads the vendored Natural Earth country/region polygons into
// admin_countries/admin_regions — upserted by their stable Natural Earth code (adm0_a3,
// adm1_code), so re-running this on redeploy is safe and never duplicates a row — and then
// backfills activity_country/activity_region for every activity ingested before this feature
// existed. New activities never need this: MatchActivity (match.go) runs at ingest time for
// each one as it's created.
//
// Registered as the `seed-admin-boundaries` subcommand (cmd/holdmytrack/main.go), mirroring
// seed-demo-customer's shape.
func SeedAdminBoundaries(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	countryIDs, err := seedCountries(ctx, pool)
	if err != nil {
		return fmt.Errorf("geo: seed countries: %w", err)
	}
	log.Info("seed-admin-boundaries: countries loaded", "count", len(countryIDs))

	regionCount, skipped, err := seedRegions(ctx, pool, countryIDs)
	if err != nil {
		return fmt.Errorf("geo: seed regions: %w", err)
	}
	log.Info("seed-admin-boundaries: regions loaded", "count", regionCount, "skipped_no_country", skipped)

	countryRows, err := backfillActivityCountries(ctx, pool)
	if err != nil {
		return fmt.Errorf("geo: backfill activity_country: %w", err)
	}
	log.Info("seed-admin-boundaries: activity_country backfilled", "rows", countryRows)

	regionRows, err := backfillActivityRegions(ctx, pool)
	if err != nil {
		return fmt.Errorf("geo: backfill activity_region: %w", err)
	}
	log.Info("seed-admin-boundaries: activity_region backfilled", "rows", regionRows)

	return nil
}

// seedCountries upserts every admin_countries row and returns adm0_a3 -> id, which seedRegions
// needs to resolve each region's parent country without a second round trip per row.
func seedCountries(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	features, err := readFeatures(countriesFile)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]int, len(features))
	for _, f := range features {
		var p countryProps
		if err := json.Unmarshal(f.Properties, &p); err != nil {
			return nil, fmt.Errorf("geo: country properties: %w", err)
		}
		// ISO_A2_EH ("extended/historical") fills in the handful of cases where Natural
		// Earth's plain ISO_A2 is "-99" for a country that does have a real ISO code —
		// Norway, France and Kosovo among them — so it's tried first.
		iso := nullableISO(p.IsoA2EH)
		if iso == nil {
			iso = nullableISO(p.IsoA2)
		}
		var id int
		err := pool.QueryRow(ctx, `
			INSERT INTO admin_countries (adm0_a3, iso_a2, name, geom)
			VALUES ($1, $2, $3, ST_SetSRID(ST_Multi(ST_GeomFromGeoJSON($4)), 4326))
			ON CONFLICT (adm0_a3) DO UPDATE SET iso_a2 = EXCLUDED.iso_a2, name = EXCLUDED.name, geom = EXCLUDED.geom
			RETURNING id
		`, p.Adm0A3, iso, p.Name, string(f.Geometry)).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("geo: upsert country %s: %w", p.Adm0A3, err)
		}
		ids[p.Adm0A3] = id
	}
	return ids, nil
}

// seedRegions upserts every admin_regions row it can resolve a parent country for. A handful
// of Natural Earth's Admin-1 rows carry an adm0_a3 with no corresponding Admin-0 country row
// at all (disputed micro-territories like Bir Tawil or Gibraltar, dropped at the coarser
// 1:50m country scale but still present in the 1:10m region layer) — those are skipped and
// counted rather than failing the whole seed, since there is no country_id to attach them to.
func seedRegions(ctx context.Context, pool *pgxpool.Pool, countryIDs map[string]int) (loaded, skipped int, err error) {
	features, err := readFeatures(regionsFile)
	if err != nil {
		return 0, 0, err
	}
	for _, f := range features {
		var p regionProps
		if err := json.Unmarshal(f.Properties, &p); err != nil {
			return 0, 0, fmt.Errorf("geo: region properties: %w", err)
		}
		countryID, ok := countryIDs[p.Adm0A3]
		if !ok {
			skipped++
			continue
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO admin_regions (country_id, adm1_code, code, name, geom)
			VALUES ($1, $2, $3, $4, ST_SetSRID(ST_Multi(ST_GeomFromGeoJSON($5)), 4326))
			ON CONFLICT (adm1_code) DO UPDATE SET
				country_id = EXCLUDED.country_id, code = EXCLUDED.code, name = EXCLUDED.name, geom = EXCLUDED.geom
		`, countryID, p.Adm1Code, nullableISO(p.Iso31662), p.Name, string(f.Geometry))
		if err != nil {
			return 0, 0, fmt.Errorf("geo: upsert region %s: %w", p.Adm1Code, err)
		}
		loaded++
	}
	return loaded, skipped, nil
}

// backfillActivityCountries and backfillActivityRegions run MatchActivity's own two queries
// (match.go) over every activity at once instead of one at a time — this only ever runs from
// the one-time seed subcommand, not per request, so there's no reason to pay per-activity
// round-trip overhead here. ON CONFLICT DO NOTHING makes re-running this safe.
func backfillActivityCountries(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `
		INSERT INTO activity_country (activity_id, country_id)
		SELECT a.id, c.id FROM activities a JOIN admin_countries c ON ST_Intersects(c.geom, a.trajectory)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func backfillActivityRegions(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `
		INSERT INTO activity_region (activity_id, region_id)
		SELECT a.id, r.id FROM activities a JOIN admin_regions r ON ST_Intersects(r.geom, a.trajectory)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
