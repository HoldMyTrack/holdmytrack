package fog

import "testing"

// maxCount is the one pure, independently-testable piece of the adaptive cap — the SQL query
// and the change-threshold/minimum-data gating around it (RecomputeHeatmapCap) are exercised
// live against a real account instead, per this project's own stated preference for
// integration-shaped behavior (IMPLEMENTATION.md). That live run against the Demo
// Customer account is also what ruled out a percentile statistic in the first place: see
// RecomputeHeatmapCap's own doc comment.

func TestMaxCount(t *testing.T) {
	if got := maxCount([]int{7}); got != 7 {
		t.Errorf("maxCount(single) = %v, want 7", got)
	}

	if got := maxCount([]int{1, 4, 2, 3}); got != 4 {
		t.Errorf("maxCount(unordered) = %v, want 4", got)
	}

	// The Demo Customer account's own shape: a long tail of once-touched tiles alongside two
	// genuinely hot ones. The max has to pick out 591, not get dragged toward the tail the way
	// a percentile over the same data does (see RecomputeHeatmapCap's doc comment for the
	// numbers this reproduces).
	skewed := []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 3, 4, 498, 591}
	if got := maxCount(skewed); got != 591 {
		t.Errorf("maxCount(skewed) = %v, want 591", got)
	}
}
