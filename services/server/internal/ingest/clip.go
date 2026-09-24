package ingest

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// Zone is one of an account's Private locations (privacy_zones, IMPLEMENTATION.md §3.7): a
// circle whose contents never leave ingest.
type Zone struct {
	Lat, Lon float64
	RadiusM  float64
}

func (z Zone) contains(p parse.Point) bool {
	return HaversineM(z.Lat, z.Lon, p.Lat, p.Lon) < z.RadiusM
}

func insideAny(zones []Zone, p parse.Point) bool {
	for _, z := range zones {
		if z.contains(p) {
			return true
		}
	}
	return false
}

// LoadZones reads an account's Private locations. Called when a job runs, never when it's
// enqueued, so a location saved while an upload waits in the queue still applies to it.
func LoadZones(ctx context.Context, pool *pgxpool.Pool, userID string) ([]Zone, error) {
	rows, err := pool.Query(ctx, `
		SELECT ST_Y(center::geometry), ST_X(center::geometry), radius_m
		FROM privacy_zones WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var zones []Zone
	for rows.Next() {
		var z Zone
		var r int
		if err := rows.Scan(&z.Lat, &z.Lon, &r); err != nil {
			return nil, err
		}
		z.RadiusM = float64(r)
		zones = append(zones, z)
	}
	return zones, rows.Err()
}

// ClipEnds is §4.1 step 3: it drops the leading points that lie inside any zone and the
// trailing points likewise, replacing each dropped run with a point interpolated onto the
// zone's boundary, so the visible track starts and ends exactly at the edge whatever the
// recording's point density. A track that only passes *through* a zone mid-way is returned
// untouched there — splitting it needs a multi-part trajectory (docs/ROADMAP.md).
//
// Returns nil when fewer than two points would survive: the activity lies entirely inside
// Private locations and has no visible geometry at all.
func ClipEnds(points []parse.Point, zones []Zone) []parse.Point {
	if len(zones) == 0 || len(points) < 2 {
		return points
	}

	first := 0
	for first < len(points) && insideAny(zones, points[first]) {
		first++
	}
	if first == len(points) {
		return nil
	}
	last := len(points) - 1
	for insideAny(zones, points[last]) {
		last--
	}

	out := make([]parse.Point, 0, last-first+3)
	if first > 0 {
		out = append(out, boundaryCrossing(zones, points[first], points[first-1]))
	}
	out = append(out, points[first:last+1]...)
	if last < len(points)-1 {
		out = append(out, boundaryCrossing(zones, points[last], points[last+1]))
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// boundaryCrossing finds the point on the segment from outside to inside where it first enters
// a zone. Bisection on the segment rather than a closed-form circle intersection: several
// overlapping zones make the boundary a union of arcs, and "inside any" is all bisection
// needs. 40 halvings takes even a 100 km segment below a millimetre.
func boundaryCrossing(zones []Zone, outside, inside parse.Point) parse.Point {
	lo, hi := 0.0, 1.0 // lo stays outside, hi stays inside
	for i := 0; i < 40; i++ {
		mid := (lo + hi) / 2
		if insideAny(zones, interpolatePoint(outside, inside, mid)) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return interpolatePoint(outside, inside, lo)
}

// interpolatePoint linearly interpolates every field a clipped endpoint needs to stay a
// valid, orderable point in the trajectory — position and time unconditionally, elevation
// and heart rate only when both sides have one (matching how a missing reading elsewhere in
// the pipeline is left nil rather than defaulted to zero).
func interpolatePoint(a, b parse.Point, t float64) parse.Point {
	p := parse.Point{
		Lat:  a.Lat + (b.Lat-a.Lat)*t,
		Lon:  a.Lon + (b.Lon-a.Lon)*t,
		Time: a.Time.Add(time.Duration(float64(b.Time.Sub(a.Time)) * t)),
	}
	if a.Elevation != nil && b.Elevation != nil {
		e := *a.Elevation + (*b.Elevation-*a.Elevation)*float32(t)
		p.Elevation = &e
	}
	if a.HeartRate != nil && b.HeartRate != nil {
		hr := int16(float64(*a.HeartRate) + (float64(*b.HeartRate)-float64(*a.HeartRate))*t)
		p.HeartRate = &hr
	}
	return p
}
