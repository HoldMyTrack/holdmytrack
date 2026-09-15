package ingest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// simplifyXY asks Postgres to run ST_SimplifyPreserveTopology and returns the surviving
// vertices' coordinates, in order.
//
// Why this isn't just "simplify the LineStringM directly": PostGIS 3.4's
// ST_SimplifyPreserveTopology drops the M ordinate entirely — verified directly against
// this stack (`SELECT GeometryType(ST_SimplifyPreserveTopology(<a LineStringM>, tol))`
// returns plain LINESTRING, not LINESTRING M), not assumed from documentation. Simplifying
// 2D-only and reattaching M afterward (matchSimplifiedTimes, below) works because
// Douglas-Peucker-style simplification only *selects a subset* of the original vertices —
// it never moves or interpolates them — so every surviving (lon, lat) exactly matches one
// of the original points.
func simplifyXY(ctx context.Context, pool *pgxpool.Pool, lons, lats []float64, toleranceDeg float64) (simpLons, simpLats []float64, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			array_agg(ST_X(pt.geom) ORDER BY pt.path),
			array_agg(ST_Y(pt.geom) ORDER BY pt.path)
		FROM ST_DumpPoints(
			ST_SimplifyPreserveTopology(
				ST_SetSRID(
					ST_MakeLine(ARRAY(
						SELECT ST_MakePoint(lon, lat)
						FROM unnest($1::float8[], $2::float8[]) AS p(lon, lat)
					)),
					4326
				),
				$3
			)
		) AS pt
	`, lons, lats, toleranceDeg).Scan(&simpLons, &simpLats)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest: simplify: %w", err)
	}
	return simpLons, simpLats, nil
}

// matchSimplifiedTimes walks both point lists in order, advancing a cursor into the
// original list to find each simplified point's source — never re-scanning from the start,
// so tracks that pass through the same coordinate twice still match the correct (earlier)
// occurrence for the first simplified point and don't get stuck reusing it for the second.
func matchSimplifiedTimes(origLons, origLats, origTs, simpLons, simpLats []float64) []float64 {
	simpTs := make([]float64, len(simpLons))
	cursor := 0
	for i := range simpLons {
		for cursor < len(origLons) && (origLons[cursor] != simpLons[i] || origLats[cursor] != simpLats[i]) {
			cursor++
		}
		if cursor >= len(origLons) {
			// Shouldn't happen given ST_SimplifyPreserveTopology's subset guarantee; fall
			// back to the last known timestamp rather than panic on a malformed match.
			if i > 0 {
				simpTs[i] = simpTs[i-1]
			}
			continue
		}
		simpTs[i] = origTs[cursor]
		cursor++
	}
	return simpTs
}
