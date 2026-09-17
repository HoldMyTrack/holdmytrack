package ingest

import (
	"testing"
	"time"
)

// The match window itself is a SQL predicate and is exercised live (a pair three seconds
// apart, and a pair well outside the tolerance). What is pure and worth pinning here is the
// ranking: §4.6's "prefer the richest record", and a tie-break that has to be total and
// stable or a re-run could flip which copy is live and churn the fog tiles underneath it.

func TestRicherThan(t *testing.T) {
	earlier := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)

	withGeometry := candidate{id: "b", hasTrajectory: true, createdAt: later}
	withoutGeometry := candidate{id: "a", hasTrajectory: false, streamChannels: 2, pointCount: 9000, createdAt: earlier}
	if !withGeometry.richerThan(withoutGeometry) {
		t.Error("geometry must win over a richer record that has none")
	}

	twoChannels := candidate{id: "a", hasTrajectory: true, streamChannels: 2, createdAt: later}
	oneChannel := candidate{id: "b", hasTrajectory: true, streamChannels: 1, pointCount: 50000, createdAt: earlier}
	if !twoChannels.richerThan(oneChannel) {
		t.Error("more stream channels must win over fewer")
	}

	// The tie-break has to be total and stable, or a re-run could flip which copy is live and
	// churn every fog tile underneath it.
	old := candidate{id: "z", hasTrajectory: true, streamChannels: 1, pointCount: 100, createdAt: earlier}
	new_ := candidate{id: "a", hasTrajectory: true, streamChannels: 1, pointCount: 100, createdAt: later}
	if !old.richerThan(new_) || new_.richerThan(old) {
		t.Error("identical copies must resolve to the older one, both ways round")
	}
	sameInstant := candidate{id: "b", hasTrajectory: true, streamChannels: 1, pointCount: 100, createdAt: earlier}
	if !old.richerThan(sameInstant) && !sameInstant.richerThan(old) {
		t.Error("copies identical to the instant must still order by id")
	}
}
