package ingest

import (
	"math"
	"testing"
	"time"

	"github.com/fitmap/fitmap/services/server/internal/parse"
)

func pt(lat, lon float64, t time.Time) parse.Point {
	return parse.Point{Lat: lat, Lon: lon, Time: t}
}

func pathLength(points []parse.Point) float64 {
	total := 0.0
	for i := 1; i < len(points); i++ {
		total += HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
	}
	return total
}

// walkedDistanceFromStart is TrimEndpoints' own contract ("walked distance", not
// straight-line) verified independently of its implementation: it finds which segment `on`
// actually lies along — the sum of the distances from both segment endpoints to `on` equals
// the segment's own length only when `on` sits on the geodesic between them, within floating
// point noise — and sums full segments up to that point plus the partial one. A folded-back
// path (this package's real fixture below has one) can make straight-line distance from an
// endpoint badly understate walked distance, which is exactly the bug this file guards
// against reintroducing.
func walkedDistanceFromStart(t *testing.T, points []parse.Point, on parse.Point) float64 {
	t.Helper()
	acc := 0.0
	for i := 1; i < len(points); i++ {
		full := HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
		toOn := HaversineM(points[i-1].Lat, points[i-1].Lon, on.Lat, on.Lon)
		onToNext := HaversineM(on.Lat, on.Lon, points[i].Lat, points[i].Lon)
		if math.Abs(toOn+onToNext-full) < 0.01 {
			return acc + toOn
		}
		acc += full
	}
	t.Fatalf("point %+v does not lie on any segment of the given path", on)
	return 0
}

// The exact 8-point track (services/server/internal/httpapi/demo_data/Car Ride 2026-09-06.gpx,
// originally "Car Ride-1.gpx") that exposed this bug: ~893m total over 8 widely-spaced points,
// one segment (p1->p2) alone almost 300m — long enough that the old whole-point-index walk
// from each end crossed itself and fell back to returning the track completely untrimmed. Its
// path also folds back near the end (a near-right-angle turn around p3), which is exactly why
// the tests below measure *walked* distance rather than straight-line — the wrong assumption
// that first caught this exact fixture returning the wrong number.
func sparseCarRideTrack() []parse.Point {
	base := time.Date(2026, 9, 6, 13, 33, 47, 0, time.UTC)
	coords := [][2]float64{
		{41.401339, -81.740215},
		{41.401329, -81.738179},
		{41.401315, -81.734594},
		{41.398916, -81.734579},
		{41.398916, -81.733716},
		{41.398903, -81.733535},
		{41.398402, -81.733596},
		{41.398344, -81.733745},
	}
	points := make([]parse.Point, len(coords))
	for i, c := range coords {
		points[i] = pt(c[0], c[1], base.Add(time.Duration(i)*10*time.Second))
	}
	return points
}

func TestTrimEndpointsSparseTrackStillTrims(t *testing.T) {
	points := sparseCarRideTrack()
	total := pathLength(points)
	if total < 850 || total > 950 {
		t.Fatalf("fixture drifted: expected ~893m total, got %.1f", total)
	}

	trimmed := TrimEndpoints(points, 200)
	if len(trimmed) < 2 {
		t.Fatalf("trim left fewer than 2 points: %d", len(trimmed))
	}

	startCut := walkedDistanceFromStart(t, points, trimmed[0])
	endCut := total - walkedDistanceFromStart(t, points, trimmed[len(trimmed)-1])

	// Walked distance from the true endpoint to the cut point should land right at 200m —
	// this is the actual bug: the old algorithm returned 0 (no trim at all) for this track.
	if startCut < 195 || startCut > 205 {
		t.Errorf("start cut %.1fm (walked) from original start, want ~200m (bug: was 0, untrimmed)", startCut)
	}
	if endCut < 195 || endCut > 205 {
		t.Errorf("end cut %.1fm (walked) from original end, want ~200m (bug: was 0, untrimmed)", endCut)
	}

	if trimmed[0].Lat == points[0].Lat && trimmed[0].Lon == points[0].Lon {
		t.Error("start point unchanged — trim silently no-op'd, the exact bug being fixed")
	}
}

func TestTrimEndpointsInterpolatesOnSparsePoints(t *testing.T) {
	// A single long segment straddling the trim radius: the cut has to land partway along
	// it, not snap to either endpoint, or a track built from only a few points would still
	// under- or over-trim depending on which side of the radius its nearest point fell.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []parse.Point{
		pt(41.0000, -81.0000, base),
		pt(41.0100, -81.0000, base.Add(time.Minute)), // ~1112m north — crosses 200m early on
		pt(41.0100, -80.9990, base.Add(2*time.Minute)),
		pt(41.0200, -80.9990, base.Add(3*time.Minute)),
	}
	total := pathLength(points)
	if total <= 400 {
		t.Fatalf("fixture too short for this test: %.1fm", total)
	}

	trimmed := TrimEndpoints(points, 200)
	if trimmed[0] == points[0] {
		t.Error("start point wasn't moved at all")
	}
	if trimmed[0].Lat == points[1].Lat && trimmed[0].Lon == points[1].Lon {
		t.Error("start point snapped to the next raw point instead of interpolating")
	}
	got := walkedDistanceFromStart(t, points, trimmed[0])
	if got < 195 || got > 205 {
		t.Errorf("interpolated start cut is %.1fm (walked) from the original start, want ~200m", got)
	}
	// Time must land strictly between the segment's two endpoints' timestamps too, so a
	// trimmed track still has a well-formed, strictly increasing time series.
	if !trimmed[0].Time.After(points[0].Time) || !trimmed[0].Time.Before(points[1].Time) {
		t.Errorf("interpolated time %v not strictly between %v and %v", trimmed[0].Time, points[0].Time, points[1].Time)
	}
}

func TestTrimEndpointsShortTrackReturnsUnchanged(t *testing.T) {
	// Genuinely too short to trim 200m off both ends (total well under 400m) — must fall
	// back to the untrimmed track, same as before this fix.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []parse.Point{
		pt(41.0000, -81.0000, base),
		pt(41.0005, -81.0000, base.Add(time.Second)),
		pt(41.0010, -81.0000, base.Add(2*time.Second)),
	}
	if pathLength(points) >= 400 {
		t.Fatalf("fixture not short enough for this test")
	}

	trimmed := TrimEndpoints(points, 200)
	if len(trimmed) != len(points) {
		t.Fatalf("expected the untrimmed track back (%d points), got %d", len(points), len(trimmed))
	}
	for i := range points {
		if trimmed[i] != points[i] {
			t.Errorf("point %d changed on a track too short to trim: got %+v, want %+v", i, trimmed[i], points[i])
		}
	}
}

func TestTrimEndpointsNoOpCases(t *testing.T) {
	points := sparseCarRideTrack()
	if got := TrimEndpoints(points, 0); len(got) != len(points) {
		t.Error("trimM=0 must be a no-op")
	}
	if got := TrimEndpoints(points, -5); len(got) != len(points) {
		t.Error("negative trimM must be a no-op")
	}
	one := points[:1]
	if got := TrimEndpoints(one, 200); len(got) != 1 {
		t.Error("fewer than 2 points must be a no-op")
	}
}
