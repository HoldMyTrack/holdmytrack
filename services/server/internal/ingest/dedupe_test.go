package ingest

import (
	"testing"
	"time"
)

// The match itself runs as a SQL predicate; overlapMatches is the same rule in Go, pinned here
// on the cases §4.6 names. The other pure piece is the ranking: "prefer the richest record",
// and a tie-break that has to be total and stable or a re-run could flip which copy is live
// and churn the fog tiles underneath it.

func TestOverlapMatches(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	s, m, h := time.Second, time.Minute, time.Hour
	cases := []struct {
		name       string
		aStart     time.Time
		aDur       time.Duration
		bStart     time.Time
		bDur       time.Duration
		wantsMatch bool
	}{
		{"identical ranges", t0, h, t0, h, true},
		{"one copy started late and stopped early", t0, h, t0.Add(20 * s), h - 30*s, true},
		{"back-to-back with a few seconds of clock skew", t0, h, t0.Add(h - 5*s), 40 * m, false},
		{"auto-detected fragment inside a long hike", t0, 2 * h, t0.Add(30 * m), 10 * m, false},
		{"day hike inside a multi-day log", t0, 72 * h, t0.Add(26 * h), h, false},
		{"exactly the threshold", t0, 100 * s, t0.Add(20 * s), 100 * s, true},
		{"just under the threshold", t0, 100 * s, t0.Add(21 * s), 100 * s, false},
		{"no duration", t0, 0, t0, h, false},
	}
	for _, c := range cases {
		if got := overlapMatches(c.aStart, c.aDur, c.bStart, c.bDur); got != c.wantsMatch {
			t.Errorf("%s: match = %v, want %v", c.name, got, c.wantsMatch)
		}
		if got := overlapMatches(c.bStart, c.bDur, c.aStart, c.aDur); got != c.wantsMatch {
			t.Errorf("%s (reversed): match = %v, want %v", c.name, got, c.wantsMatch)
		}
	}
}

// Every pair overlapMatches accepts must start within dedupeStartSlack of the incoming
// activity's duration, or the SQL prefilter would drop a real match before the exact test.
func TestStartSlackCoversEveryMatch(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	d := 1000 * time.Second
	for other := 700 * time.Second; other <= 1300*time.Second; other += 10 * time.Second {
		for off := -400 * time.Second; off <= 400*time.Second; off += time.Second {
			if !overlapMatches(t0, d, t0.Add(off), other) {
				continue
			}
			if abs := off.Abs(); float64(abs) > float64(d)*dedupeStartSlack {
				t.Fatalf("match at start offset %v (other %v) is outside the slack of %v", off, other, time.Duration(float64(d)*dedupeStartSlack))
			}
		}
	}
}

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
