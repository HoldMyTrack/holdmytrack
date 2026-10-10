package httpapi

import (
	"net/http"
	"testing"
	"time"
)

// Trends returns every week or month its window touches, an empty one as zeroes, so a chart
// drawn one bar per period spans the whole window (docs/SPEC.md FR-9 behavior 1).
func TestTrendsFillEmptyPeriods(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	here := [2]float64{-81.60, 41.30}
	for _, at := range []time.Time{
		time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC), // Wednesday of the week of January 12
		time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),  // Thursday of the week of March 30
	} {
		d.newActivity(me, testActivity{activityType: "running", distanceMeters: 5000, durationSecs: 1800, at: &here, startedAt: at})
	}

	trends := func(query string) []trendPeriod {
		t.Helper()
		var resp activityTrendsResponse
		d.decode(d.do(me, "GET", "/v1/activities/trends?"+query, nil), http.StatusOK, &resp)
		return resp.Periods
	}

	// January 1 (a Thursday) to April 30: the weeks of December 29 to April 27.
	weeks := trends("bucket=week&from=2026-01-01&to=2026-04-30")
	if len(weeks) != 18 || weeks[0].PeriodStart != "2025-12-29" || weeks[17].PeriodStart != "2026-04-27" {
		t.Fatalf("weeks %+v, want the 18 weeks of December 29 to April 27", weeks)
	}
	active := 0
	for i, w := range weeks {
		if w.Count == 0 {
			if w.DistanceMeters != 0 || w.MovingSeconds != 0 || w.ElevationGainM != 0 {
				t.Errorf("empty week %s: %+v, want zeroes", w.PeriodStart, w)
			}
			continue
		}
		active++
		if want := map[int]string{2: "2026-01-12", 13: "2026-03-30"}[i]; w.PeriodStart != want || w.Count != 1 || w.DistanceMeters != 5000 {
			t.Errorf("week %d: %+v, want one 5 km run in the week of %s", i, w, want)
		}
	}
	if active != 2 {
		t.Errorf("%d active weeks, want 2", active)
	}

	months := trends("bucket=month&from=2026-01-01&to=2026-04-30")
	got := []int64{}
	for _, m := range months {
		got = append(got, m.Count)
	}
	if len(months) != 4 || months[0].PeriodStart != "2026-01-01" || months[3].PeriodStart != "2026-04-01" || got[0] != 1 || got[1] != 0 || got[2] != 0 || got[3] != 1 {
		t.Errorf("months %+v, want January to April with a run in the first and the last", months)
	}

	// A window with nothing in it still lists its periods.
	if empty := trends("bucket=month&from=2025-06-15&to=2025-08-15"); len(empty) != 3 || empty[0].PeriodStart != "2025-06-01" || empty[0].Count != 0 {
		t.Errorf("empty window %+v, want June to August, all zero", empty)
	}
}
