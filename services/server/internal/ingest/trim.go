package ingest

import (
	"time"

	"github.com/fitmap/fitmap/services/server/internal/parse"
)

// TrimEndpoints drops points within trimM meters of walked distance from each end of the
// track — IMPLEMENTATION.md §7's endpoint trim, applied unconditionally
// (§7: "non-negotiable"). There's no privacy_zones data to clip against yet (no UI),
// so that half of §4.1 step 3 is a no-op for now; this half still applies.
//
// The cut point is interpolated to fall exactly at trimM along the crossing segment, not
// snapped to the nearest existing point — a track recorded with few, widely-spaced points
// (a short errand drive logged every 100+ meters, say) would otherwise let a single segment
// overshoot the trim radius by a large margin, silently leaving far more than trimM of real
// distance unclipped at that end. Interpolating means the result depends only on trimM and
// the track's real geometry, never on how sparse the original recording happened to be.
//
// Exported so internal/fog can apply the exact same trim when re-parsing an activity's raw
// payload to render or re-render a tile (§4.2) — coverage must reflect the same privacy-
// clipped points the activity itself was persisted from, not the untrimmed raw file.
func TrimEndpoints(points []parse.Point, trimM int) []parse.Point {
	if trimM <= 0 || len(points) < 2 {
		return points
	}

	total := 0.0
	for i := 1; i < len(points); i++ {
		total += HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
	}
	if total <= 2*float64(trimM) {
		// The track's actual walked length, not just where the two cuts below happen to
		// land — genuinely too short to trim both ends without them meeting or crossing.
		// Keep the endpoints rather than emit an empty/degenerate activity: an
		// over-aggressive trim on a short track is a worse failure mode than under-trimming
		// by a few meters.
		return points
	}

	beforeIdx, startPt := cutFromStart(points, float64(trimM))
	afterIdx, endPt := cutFromEnd(points, float64(trimM))

	midStart, midEnd := beforeIdx+1, afterIdx
	if midStart > midEnd {
		midStart = midEnd
	}
	trimmed := make([]parse.Point, 0, midEnd-midStart+2)
	trimmed = append(trimmed, startPt)
	trimmed = append(trimmed, points[midStart:midEnd]...)
	trimmed = append(trimmed, endPt)
	return trimmed
}

// cutFromStart walks forward from points[0] and returns the index of the last point fully
// inside the trimmed-off zone (everything at index <= beforeIdx is dropped) plus a point
// interpolated to fall exactly trimM along the segment that crosses it.
func cutFromStart(points []parse.Point, trimM float64) (beforeIdx int, cut parse.Point) {
	acc := 0.0
	for i := 1; i < len(points); i++ {
		seg := HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
		if acc+seg >= trimM {
			return i - 1, interpolatePoint(points[i-1], points[i], fraction(trimM-acc, seg))
		}
		acc += seg
	}
	// Unreachable given the caller's total-length guard above, but fall back to the last
	// point rather than panic if it's ever called without that guard.
	last := len(points) - 1
	return last - 1, points[last]
}

// cutFromEnd is cutFromStart's mirror, walking backward from the last point. Everything at
// index >= afterIdx is dropped.
func cutFromEnd(points []parse.Point, trimM float64) (afterIdx int, cut parse.Point) {
	acc := 0.0
	n := len(points)
	for i := n - 1; i > 0; i-- {
		seg := HaversineM(points[i].Lat, points[i].Lon, points[i-1].Lat, points[i-1].Lon)
		if acc+seg >= trimM {
			return i, interpolatePoint(points[i], points[i-1], fraction(trimM-acc, seg))
		}
		acc += seg
	}
	return 1, points[0]
}

// fraction is how far along a segment of length segM the trim radius's own remaining
// distance (remainingM) falls, clamped to [0, 1] — segments are never zero-length here (a
// zero-length "segment" can't be the one whose cumulative sum first reaches trimM), but the
// guard keeps this safe if that assumption ever stops holding.
func fraction(remainingM, segM float64) float64 {
	if segM <= 0 {
		return 0
	}
	t := remainingM / segM
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// interpolatePoint linearly interpolates every field a trimmed endpoint needs to stay a
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
