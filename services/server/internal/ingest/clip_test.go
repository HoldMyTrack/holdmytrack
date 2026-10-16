package ingest

import (
	"math"
	"slices"
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

// expectedStart is clipStart's cut for a dense track leaving zone once: the first point at or
// past the drawn distance along the track from the crossing, or the one before it when that
// lies more than a radius on.
func expectedStart(points []parse.Point, zone Zone, end byte) parse.Point {
	first := 0
	for zone.contains(points[first]) {
		first++
	}
	d := trimFraction(trimSeed([]Zone{zone}, points[first-1], end), 0) * zone.RadiusM
	prev := boundaryCrossing([]Zone{zone}, points[first], points[first-1])
	walked := 0.0
	for i, p := range points[first:] {
		walked += HaversineM(prev.Lat, prev.Lon, p.Lat, p.Lon)
		if walked >= d {
			if walked > zone.RadiusM && i > 0 {
				return prev
			}
			return p
		}
		prev = p
	}
	panic("fixture: track too short")
}

func TestClipEndsStartInside(t *testing.T) {
	points := line(41.4, -81.7, 201, 5) // 1 km north, 5 m apart
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 175, Salt: "a"}
	got := ClipEnds(points, []Zone{zone})

	if want := expectedStart(points, zone, 's'); got[0] != want {
		t.Errorf("first point %+v, want %+v", got[0], want)
	}
	if d := distToZone(zone, got[0]); d < 175 || d > 350 {
		t.Errorf("first point %.1f m from center, want between the edge and a radius past it", d)
	}
	if got[len(got)-1] != points[len(points)-1] {
		t.Error("end outside every zone was clipped")
	}
}

func TestClipEndsEndInside(t *testing.T) {
	points := line(41.4, -81.7, 201, 5)
	end := points[len(points)-1]
	zone := Zone{Lat: end.Lat, Lon: end.Lon, RadiusM: 120, Salt: "a"}
	got := ClipEnds(points, []Zone{zone})

	if got[0] != points[0] {
		t.Error("start outside every zone was clipped")
	}
	if want := expectedStart(reversed(points), zone, 'e'); got[len(got)-1] != want {
		t.Errorf("last point %+v, want %+v", got[len(got)-1], want)
	}
}

// The point of the extra stretch: tracks leaving home in every direction don't end on one
// circle. Each is cut at a different distance, and every end is a recorded point.
func TestClipEndsSpreadsEnds(t *testing.T) {
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 200, Salt: "home"}
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	lo, hi := math.Inf(1), 0.0
	for k := 0; k < 24; k++ {
		bearing := float64(k) * math.Pi / 12
		points := make([]parse.Point, 200)
		for i := range points {
			m := float64(i) * 5
			points[i] = pt(41.4+m*math.Cos(bearing)/111195, -81.7+m*math.Sin(bearing)/(111195*math.Cos(41.4*math.Pi/180)),
				t0.Add(time.Duration(k)*time.Hour+time.Duration(i)*time.Second))
		}
		got := ClipEnds(points, []Zone{zone})
		if !slices.Contains(points, got[0]) {
			t.Fatalf("track %d starts at %+v, not a recorded point", k, got[0])
		}
		d := distToZone(zone, got[0])
		lo, hi = math.Min(lo, d), math.Max(hi, d)
	}
	if lo < 200 || hi > 400 {
		t.Errorf("ends %.0f–%.0f m from center, want between the edge and a radius past it", lo, hi)
	}
	if hi-lo < 100 {
		t.Errorf("ends %.0f–%.0f m from center: too close to one circle", lo, hi)
	}
}

// The same recording clips the same way every time, and another location's salt cuts it
// elsewhere.
func TestClipEndsStableAndSalted(t *testing.T) {
	points := line(41.4, -81.7, 201, 5)
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 300, Salt: "a"}
	first := ClipEnds(points, []Zone{zone})[0]
	if again := ClipEnds(points, []Zone{zone})[0]; again != first {
		t.Errorf("second clip starts at %+v, first at %+v", again, first)
	}
	differs := false
	for _, salt := range []string{"b", "c", "d", "e"} {
		zone.Salt = salt
		if ClipEnds(points, []Zone{zone})[0] != first {
			differs = true
		}
	}
	if !differs {
		t.Error("every salt cut the track at the same point")
	}
}

// Clipping a clipped track again changes nothing: its ends already lie outside every zone. A
// track edit relies on it (TrackEdit.ApplyClipped).
func TestClipEndsIdempotent(t *testing.T) {
	points := line(41.4, -81.7, 201, 5)
	end := points[len(points)-1]
	zones := []Zone{{Lat: 41.4, Lon: -81.7, RadiusM: 150, Salt: "a"}, {Lat: end.Lat, Lon: end.Lon, RadiusM: 150, Salt: "b"}}
	once := ClipEnds(points, zones)
	if twice := ClipEnds(once, zones); !slices.Equal(once, twice) {
		t.Errorf("clipping again changed the track: %d points, then %d", len(once), len(twice))
	}
}

func TestClipEndsBothEndsInSameZone(t *testing.T) {
	// Out and back: 100 points north, then the same points south again.
	out := line(41.4, -81.7, 101, 10)
	back := make([]parse.Point, 0, 100)
	for i := 99; i >= 0; i-- {
		p := out[i]
		p.Time = out[100].Time.Add(time.Duration(100-i) * time.Second)
		back = append(back, p)
	}
	points := append(out, back...)
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 250, Salt: "a"}
	got := ClipEnds(points, []Zone{zone})

	for _, p := range got {
		if zone.contains(p) {
			t.Errorf("point %+v is inside the zone", p)
		}
	}
	if got[0].Time == got[len(got)-1].Time {
		t.Fatal("fixture: track hidden entirely")
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

// Sparse recording: the only segment leaving the zone is 600 m long. Its far end lies past a
// radius beyond the edge, so the cut is placed on the segment rather than at that point, and
// never more than a radius past the edge.
func TestClipEndsSparsePoints(t *testing.T) {
	points := line(41.4, -81.7, 4, 600)
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 100, Salt: "a"}
	got := ClipEnds(points, []Zone{zone})
	if len(got) != 4 {
		t.Fatalf("got %d points, want 4 (cut + 3 originals)", len(got))
	}
	if d := distToZone(zone, got[0]); d < 100 || d > 200 {
		t.Errorf("first point %.1f m from center, want 100–200", d)
	}
}

func TestClipEndsOverlappingZones(t *testing.T) {
	points := line(41.4, -81.7, 201, 5)
	a := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 100}
	b := Zone{Lat: points[30].Lat, Lon: -81.7, RadiusM: 100} // covers 50–250 m, overlapping a
	got := ClipEnds(points, []Zone{a, b})
	if d := distToZone(b, got[0]); d < 100 || d > 200 {
		t.Errorf("first point %.1f m from the outer zone's center, want within a radius past its far edge", d)
	}
}

// A track that leaves, curves back in and leaves again: an end drawn inside the zone is cut
// again where the track next leaves.
func TestClipEndsReentry(t *testing.T) {
	zone := Zone{Lat: 41.4, Lon: -81.7, RadiusM: 100, Salt: "a"}
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	var points []parse.Point
	// North to 105 m, back south to 50 m, then north for 1 km.
	for _, m := range append(append(seq(0, 105, 5), seq(100, 50, -5)...), seq(55, 1000, 5)...) {
		points = append(points, pt(41.4+m/111195, -81.7, t0.Add(time.Duration(len(points))*time.Second)))
	}
	for _, salt := range []string{"a", "b", "c", "d", "e", "f"} {
		zone.Salt = salt
		got := ClipEnds(points, []Zone{zone})
		if zone.contains(got[0]) {
			t.Errorf("salt %s: track starts inside the zone, %.1f m from center", salt, distToZone(zone, got[0]))
		}
	}
}

func seq(from, to, step float64) []float64 {
	var out []float64
	for m := from; (step > 0 && m <= to) || (step < 0 && m >= to); m += step {
		out = append(out, m)
	}
	return out
}

func TestClipEndsEntirelyInsideReturnsNil(t *testing.T) {
	points := line(41.4, -81.7, 10, 10) // 90 m long
	if got := ClipEnds(points, []Zone{{Lat: 41.4, Lon: -81.7, RadiusM: 500}}); got != nil {
		t.Fatalf("got %d points, want nil for a fully hidden track", len(got))
	}
}
