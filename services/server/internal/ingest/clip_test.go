package ingest

import (
	"math"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

func pt(lat, lon float64, t time.Time) parse.Point {
	return parse.Point{Lat: lat, Lon: lon, Time: t}
}

// line is n points heading north from (lat0, lon0), stepM metres apart, one second apart.
func line(lat0, lon0 float64, n int, stepM float64) []parse.Point {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	dLat := stepM / 111195.0 // metres per degree of latitude on earthRadiusM
	points := make([]parse.Point, n)
	for i := range points {
		points[i] = pt(lat0+float64(i)*dLat, lon0, t0.Add(time.Duration(i)*time.Second))
	}
	return points
}

func distToZone(z Zone, p parse.Point) float64 {
	return HaversineM(z.Lat, z.Lon, p.Lat, p.Lon)
}

func TestClipEndsNoZonesKeepsEverything(t *testing.T) {
	points := line(41.4, -81.7, 20, 50)
	if got := ClipEnds(points, nil); len(got) != len(points) {
		t.Fatalf("got %d points, want %d", len(got), len(points))
	}
}

func TestClipEndsStartInside(t *testing.T) {
	points := line(41.4, -81.7, 21, 50) // 1 km north
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 175}
	got := ClipEnds(points, []Zone{zone})

	// Points 0..3 (0–150 m) are inside; the first kept point is the crossing at 175 m.
	if d := distToZone(zone, got[0]); math.Abs(d-175) > 0.01 {
		t.Errorf("first point %.3f m from center, want 175 on the boundary", d)
	}
	if got[1] != points[4] {
		t.Errorf("second point = %+v, want the first original point outside (%+v)", got[1], points[4])
	}
	if got[len(got)-1] != points[len(points)-1] {
		t.Error("end outside every zone was clipped")
	}
	if len(got) != len(points)-4+1 {
		t.Errorf("got %d points, want %d", len(got), len(points)-3)
	}
	// Time is interpolated with position: 175 m at 50 m/s-per-point is 3.5 s in.
	if want := points[0].Time.Add(3500 * time.Millisecond); math.Abs(float64(got[0].Time.Sub(want))) > float64(time.Millisecond) {
		t.Errorf("crossing time %v, want ~%v", got[0].Time, want)
	}
}

func TestClipEndsEndInside(t *testing.T) {
	points := line(41.4, -81.7, 21, 50)
	end := points[len(points)-1]
	zone := Zone{Lat: end.Lat, Lon: end.Lon, RadiusM: 120}
	got := ClipEnds(points, []Zone{zone})

	if got[0] != points[0] {
		t.Error("start outside every zone was clipped")
	}
	if d := distToZone(zone, got[len(got)-1]); math.Abs(d-120) > 0.01 {
		t.Errorf("last point %.3f m from center, want 120 on the boundary", d)
	}
}

func TestClipEndsBothEndsInSameZone(t *testing.T) {
	// Out and back: 10 points north, then the same 10 points south again.
	out := line(41.4, -81.7, 11, 100)
	back := make([]parse.Point, 0, 10)
	for i := 9; i >= 0; i-- {
		p := out[i]
		p.Time = out[10].Time.Add(time.Duration(10-i) * time.Second)
		back = append(back, p)
	}
	points := append(out, back...)
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 250}
	got := ClipEnds(points, []Zone{zone})

	for _, p := range []parse.Point{got[0], got[len(got)-1]} {
		if d := distToZone(zone, p); math.Abs(d-250) > 0.01 {
			t.Errorf("endpoint %.3f m from center, want 250", d)
		}
	}
	for _, p := range got[1 : len(got)-1] {
		if zone.contains(p) {
			t.Errorf("interior point %+v is inside the zone", p)
		}
	}
}

// The case the old fixed endpoint trim got wrong: a track passing through a zone (not starting
// or ending in it) is left alone in v1, rather than bridged or partially hidden.
func TestClipEndsPassThroughUntouched(t *testing.T) {
	points := line(41.4, -81.7, 21, 50)
	mid := points[10]
	got := ClipEnds(points, []Zone{{Lat: mid.Lat, Lon: mid.Lon, RadiusM: 100}})
	if len(got) != len(points) {
		t.Fatalf("got %d points, want all %d", len(got), len(points))
	}
}

// Sparse recording: the only segment leaving the zone is 600 m long. The crossing still lands
// on the boundary rather than snapping to whichever real point is nearest.
func TestClipEndsSparsePoints(t *testing.T) {
	points := line(41.4, -81.7, 4, 600)
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 100}
	got := ClipEnds(points, []Zone{zone})
	if len(got) != 4 {
		t.Fatalf("got %d points, want 4 (crossing + 3 originals)", len(got))
	}
	if d := distToZone(zone, got[0]); math.Abs(d-100) > 0.01 {
		t.Errorf("first point %.3f m from center, want 100", d)
	}
}

func TestClipEndsOverlappingZones(t *testing.T) {
	points := line(41.4, -81.7, 21, 50)
	a := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 100}
	b := Zone{Lat: points[3].Lat, Lon: -81.7, RadiusM: 100} // covers 50–250 m, overlapping a
	got := ClipEnds(points, []Zone{a, b})
	if d := distToZone(b, got[0]); math.Abs(d-100) > 0.01 {
		t.Errorf("first point %.3f m from the outer zone's center, want 100 (its far edge)", d)
	}
	if insideAny([]Zone{a, b}, got[1]) {
		t.Error("second point still inside a zone")
	}
}

func TestClipEndsEntirelyInsideReturnsNil(t *testing.T) {
	points := line(41.4, -81.7, 10, 10) // 90 m long
	if got := ClipEnds(points, []Zone{{Lat: 41.4, Lon: -81.7, RadiusM: 500}}); got != nil {
		t.Fatalf("got %d points, want nil for a fully hidden track", len(got))
	}
}

// One real point outside, both neighbours inside: the two crossings still make a line.
func TestClipEndsSingleOutsidePoint(t *testing.T) {
	points := line(41.4, -81.7, 3, 100)
	zones := []Zone{
		{Lat: points[0].Lat, Lon: -81.7, RadiusM: 60},
		{Lat: points[2].Lat, Lon: -81.7, RadiusM: 60},
	}
	got := ClipEnds(points, zones)
	if len(got) != 3 || got[1] != points[1] {
		t.Fatalf("got %+v, want [crossing, middle point, crossing]", got)
	}
}
