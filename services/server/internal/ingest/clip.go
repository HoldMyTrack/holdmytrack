package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// Zone is one of an account's Private locations (privacy_zones, IMPLEMENTATION.md §3.7): a
// circle whose contents never leave ingest. Salt is its secret, random per location, that
// seeds how far past its edge a track's end is cut (trimSeed).
type Zone struct {
	Lat, Lon float64
	RadiusM  float64
	Salt     string
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
		SELECT ST_Y(center::geometry), ST_X(center::geometry), radius_m, salt::text
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
		if err := rows.Scan(&z.Lat, &z.Lon, &r, &z.Salt); err != nil {
			return nil, err
		}
		z.RadiusM = float64(r)
		zones = append(zones, z)
	}
	return zones, rows.Err()
}

// ClipEnds is §4.1 step 3: it drops the leading points that lie inside any zone and the
// trailing points likewise, and then a further stretch past the zone's edge (clipStart), so
// the visible ends don't trace the circle around the place it hides. A track that only passes
// *through* a zone mid-way is returned untouched there — splitting it needs a multi-part
// trajectory (docs/ROADMAP.md).
//
// Returns nil when fewer than two points would survive: the activity lies entirely inside
// Private locations, or so close to one that the extra stretch takes the rest.
func ClipEnds(points []parse.Point, zones []Zone) []parse.Point {
	if len(zones) == 0 || len(points) < 2 {
		return points
	}

	out := clipStart(points, zones, 's')
	if len(out) >= 2 && insideAny(zones, out[len(out)-1]) {
		out = reversed(clipStart(reversed(out), zones, 'e'))
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// clipStart drops the leading run of points inside any zone, then walks on along the track
// from the zone's edge for a distance between 0 and the zone's radius, and starts the track at
// the first recorded point it reaches. Ends placed exactly on the edge would let three tracks
// fix the circle, and its center with it; spread over a band past the edge, and cut at real
// points rather than ones placed on a curve, they fit no circle.
//
// The distance is random but fixed for the end (trimFraction), so reprocessing the activity
// gives the same track: a new draw each time would let the ends seen over several reprocesses
// average back to the edge. No end is cut more than a radius past the edge, which keeps every
// end within twice the radius of the center, where AffectedActivities looks for it: when the
// point reached lies further on, the recorded point before it is the end instead, and when
// there is none — a sparse recording's first segment out — the track is cut at the distance
// itself. An end that lands back inside
// a zone, on a track curving back in, starts over from where the track leaves it again.
func clipStart(points []parse.Point, zones []Zone, end byte) []parse.Point {
	first := 0
	for first < len(points) && insideAny(zones, points[first]) {
		first++
	}
	if first == len(points) {
		return nil
	}
	if first == 0 {
		return points
	}

	seed := trimSeed(zones, points[first-1], end)
	in, rest := points[first-1], points[first:]
	for step := 0; ; step++ {
		r := radiusAt(zones, in)
		d := trimFraction(seed, step) * r
		prev := boundaryCrossing(zones, rest[0], in)
		walked, seg := 0.0, 0.0
		i := 0
		for ; i < len(rest); i++ {
			seg = HaversineM(prev.Lat, prev.Lon, rest[i].Lat, rest[i].Lon)
			if walked+seg >= d {
				break
			}
			walked += seg
			prev = rest[i]
		}
		if i == len(rest) {
			return nil
		}

		out := rest[i:]
		if walked+seg > r {
			if i > 0 {
				out = rest[i-1:]
			} else {
				cut := interpolatePoint(prev, rest[i], (d-walked)/seg)
				out = append([]parse.Point{cut}, rest[i:]...)
			}
		}
		if !insideAny(zones, out[0]) {
			return out
		}
		j := 1
		for j < len(out) && insideAny(zones, out[j]) {
			j++
		}
		if j == len(out) {
			return nil
		}
		in, rest = out[j-1], out[j:]
	}
}

// trimSeed is what an end's extra stretch is drawn from: the last hidden point (position and
// time, never shown) with the salts of the zones it lies in. The salts are what keep the draw
// secret. Anyone holding the visible track and this code could otherwise try candidate times
// for the hidden point, and each guess would give back a candidate edge.
func trimSeed(zones []Zone, p parse.Point, end byte) []byte {
	var salts []string
	for _, z := range zones {
		if z.contains(p) {
			salts = append(salts, z.Salt)
		}
	}
	sort.Strings(salts)
	h := sha256.New()
	for _, s := range salts {
		h.Write([]byte(s))
	}
	fmt.Fprintf(h, "|%d|%.7f|%.7f|%c", p.Time.UnixMilli(), p.Lat, p.Lon, end)
	return h.Sum(nil)
}

// trimFraction is the step'th draw from seed, uniform in [0, 1).
func trimFraction(seed []byte, step int) float64 {
	h := sha256.New()
	h.Write(seed)
	binary.Write(h, binary.BigEndian, int64(step))
	return float64(binary.BigEndian.Uint64(h.Sum(nil))>>11) / (1 << 53)
}

// radiusAt is the largest radius among the zones containing p.
func radiusAt(zones []Zone, p parse.Point) float64 {
	r := 0.0
	for _, z := range zones {
		if z.contains(p) && z.RadiusM > r {
			r = z.RadiusM
		}
	}
	return r
}

func reversed(points []parse.Point) []parse.Point {
	out := make([]parse.Point, len(points))
	for i, p := range points {
		out[len(points)-1-i] = p
	}
	return out
}

// boundaryCrossing finds the point on the segment from outside to inside where it first enters
// a zone: where clipStart starts measuring its extra stretch from. Bisection on the segment rather than a closed-form circle intersection: several
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
// only when both sides have one (matching how a missing reading elsewhere in the pipeline is
// left nil rather than defaulted to zero).
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
	return p
}
