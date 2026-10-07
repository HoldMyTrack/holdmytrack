package httpapi

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"
)

// Every day an activity is grouped or filtered by is the local date where it was recorded
// (IMPLEMENTATION.md §4.30), not the account's: for an account on New York time, a run at
// 07:30 on 10 March in Tokyo (22:30 on the 9th in New York) is on the 10th everywhere.
func TestActivitiesGroupByTheirOwnTimezone(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET timezone = 'America/New_York' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	here := [2]float64{-81.60, 41.30}
	tokyo := d.newActivity(me, testActivity{activityType: "running", distanceMeters: 5000, durationSecs: 1800, at: &here,
		startedAt: time.Date(2026, 3, 9, 22, 30, 0, 0, time.UTC), timezone: "Asia/Tokyo"})
	// 23:30 on 9 March in New York, the account's own zone: the 10th in UTC, the 9th here.
	home := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 2000, durationSecs: 1200, at: &here,
		startedAt: time.Date(2026, 3, 10, 3, 30, 0, 0, time.UTC)})

	list := func(query string) []string {
		t.Helper()
		var resp activitiesResponse
		d.decode(d.do(me, "GET", "/v1/activities?"+query, nil), http.StatusOK, &resp)
		ids := []string{}
		for _, a := range resp.Activities {
			ids = append(ids, a.ID)
		}
		return ids
	}
	for query, want := range map[string][]string{
		"from=2026-03-10&to=2026-03-10": {tokyo},
		"from=2026-03-09&to=2026-03-09": {home},
		"from=2026-03-09":               {home, tokyo},
		"to=2026-03-09":                 {home},
	} {
		if got := list(query); !slices.Equal(got, want) {
			t.Errorf("list ?%s = %v, want %v", query, got, want)
		}
	}

	var summary activitySummaryResponse
	d.decode(d.do(me, "GET", "/v1/activities/summary?from=2026-03-10&to=2026-03-10", nil), http.StatusOK, &summary)
	if summary.Count != 1 || summary.DistanceMeters != 5000 {
		t.Errorf("summary of the 10th %+v, want the Tokyo run alone", summary)
	}

	var hist activityHistogramResponse
	d.decode(d.do(me, "GET", "/v1/activities/histogram?from=2026-03-01&to=2026-03-31", nil), http.StatusOK, &hist)
	if len(hist.Buckets) != 2 || hist.Buckets[0].Date != "2026-03-09" || hist.Buckets[1].Date != "2026-03-10" || hist.Earliest != "2026-03-09" {
		t.Errorf("histogram %+v, want the 9th and the 10th, earliest the 9th", hist)
	}
	d.decode(d.do(me, "GET", "/v1/activities/histogram?days=5&before=2026-03-10", nil), http.StatusOK, &hist)
	if len(hist.Buckets) != 1 || hist.Buckets[0].Date != "2026-03-09" {
		t.Errorf("days before the 10th %+v, want the 9th alone", hist.Buckets)
	}

	var stats activityGraphStatsResponse
	d.decode(d.do(me, "GET", "/v1/activities/graph-stats?year=2026", nil), http.StatusOK, &stats)
	if stats.YearStats.ActiveDays != 2 || stats.YearStats.LongestStreakDays != 2 {
		t.Errorf("graph stats %+v, want two days in a row", stats.YearStats)
	}

	var trends activityTrendsResponse
	d.decode(d.do(me, "GET", "/v1/activities/trends?bucket=week&from=2026-03-01&to=2026-03-31", nil), http.StatusOK, &trends)
	// Monday the 9th's week holds both.
	if len(trends.Periods) != 1 || trends.Periods[0].PeriodStart != "2026-03-09" || trends.Periods[0].Count != 2 {
		t.Errorf("trends %+v, want both in the week of the 9th", trends.Periods)
	}
}
