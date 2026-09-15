package ingest

import (
	"testing"

	"github.com/fitmap/fitmap/services/server/internal/parse"
)

// TestBestEffortCurveFindsTheFastSegment: a 30s fast burst (10 m/s) followed by a 30s slow
// segment (1 m/s). A 30s window should find exactly the burst, not something diluted by the
// slow segment that happens to also span >= 30s.
func TestBestEffortCurveFindsTheFastSegment(t *testing.T) {
	elapsedS := []int32{0, 10, 20, 30, 40, 50, 60}
	cum := []float64{0, 100, 200, 300, 310, 320, 330} // +100m/10s (burst), then +10m/10s (slow)

	results := bestEffortCurve("pace", elapsedS, cum)

	byWindow := make(map[int32]float32)
	for _, r := range results {
		byWindow[r.windowS] = r.value
	}

	if v, ok := byWindow[30]; !ok || v != 10 {
		t.Fatalf("window 30s: want 10 m/s (the burst), got %v (found=%v)", v, ok)
	}
	// The whole-activity window blends both segments, so it must be strictly worse than the
	// burst alone — otherwise the two-pointer scan isn't actually finding the tightest window.
	if v, ok := byWindow[60]; !ok || v >= 10 {
		t.Fatalf("window 60s: want < 10 m/s (blended average), got %v (found=%v)", v, ok)
	}
}

// TestBestEffortCurveSkipsWindowsLongerThanTheActivity: a 60s-long activity has no valid
// position for a 3600s window, so it must be omitted from the result, not returned as some
// fabricated value.
func TestBestEffortCurveSkipsWindowsLongerThanTheActivity(t *testing.T) {
	elapsedS := []int32{0, 10, 20, 30, 40, 50, 60}
	cum := []float64{0, 100, 200, 300, 310, 320, 330}

	results := bestEffortCurve("pace", elapsedS, cum)
	for _, r := range results {
		if r.windowS == 3600 {
			t.Fatalf("window 3600s should have been skipped (activity is only 60s), got %v", r)
		}
	}
}

// TestComputeSplitsFindsTheFastSegment: a fast 1000m opener (5 m/s, 200s) followed by a much
// slower 5000m (1 m/s, 5000s) — total 6000m. The 1K split should be exactly the fast opener;
// the 5K split should span the slow segment alone (the only 5000m-or-more window there is);
// there is no 10K (or half/full marathon) row at all, since the activity is only 6000m long.
func TestComputeSplitsFindsTheFastSegment(t *testing.T) {
	elapsedS := []int32{0, 200, 5200}
	distM := []float32{0, 1000, 6000}

	results := computeSplits(elapsedS, distM)

	byDistance := make(map[int32]int32)
	for _, r := range results {
		byDistance[r.distanceM] = r.seconds
	}

	if v, ok := byDistance[1000]; !ok || v != 200 {
		t.Fatalf("1K split: want 200s (the fast opener), got %v (found=%v)", v, ok)
	}
	if v, ok := byDistance[5000]; !ok || v != 5000 {
		t.Fatalf("5K split: want 5000s (the slow segment alone), got %v (found=%v)", v, ok)
	}
	for _, d := range []int32{10000, 21097, 42195} {
		if _, ok := byDistance[d]; ok {
			t.Fatalf("distance %dm should have been skipped (activity is only 6000m), got a row", d)
		}
	}
}

func point(hr *int16) parse.Point {
	return parse.Point{HeartRate: hr}
}

func int16p(v int16) *int16 { return &v }

// TestComputeBestEffortsSkipsHeartRateWhenAnyPointIsMissingIt: one point out of several with
// a nil HeartRate must suppress every heartrate result, not just that point's own window —
// a curve with silently-skipped points would understate effort rather than reporting
// nothing at all.
func TestComputeBestEffortsSkipsHeartRateWhenAnyPointIsMissingIt(t *testing.T) {
	elapsedS := []int32{0, 10, 20, 30, 40, 50, 60}
	distM := []float32{0, 100, 200, 300, 310, 320, 330}
	points := []parse.Point{
		point(int16p(140)),
		point(int16p(142)),
		point(nil), // the gap
		point(int16p(145)),
		point(int16p(146)),
		point(int16p(147)),
		point(int16p(148)),
	}

	results := computeBestEfforts(points, elapsedS, distM)

	sawPace := false
	for _, r := range results {
		if r.metric == "heartrate" {
			t.Fatalf("expected no heartrate results with a gap in HR data, got %v", r)
		}
		if r.metric == "pace" {
			sawPace = true
		}
	}
	if !sawPace {
		t.Fatal("expected pace results even though heart-rate data is incomplete")
	}
}

// TestComputeBestEffortsIncludesHeartRateWhenComplete: full HR coverage produces heartrate
// results alongside pace.
func TestComputeBestEffortsIncludesHeartRateWhenComplete(t *testing.T) {
	elapsedS := []int32{0, 10, 20, 30, 40, 50, 60}
	distM := []float32{0, 100, 200, 300, 310, 320, 330}
	points := []parse.Point{
		point(int16p(140)),
		point(int16p(142)),
		point(int16p(144)),
		point(int16p(145)),
		point(int16p(146)),
		point(int16p(147)),
		point(int16p(148)),
	}

	results := computeBestEfforts(points, elapsedS, distM)

	sawHR := false
	for _, r := range results {
		if r.metric == "heartrate" {
			sawHR = true
		}
	}
	if !sawHR {
		t.Fatal("expected heartrate results with complete HR coverage")
	}
}
