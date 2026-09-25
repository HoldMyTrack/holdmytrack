package httpapi

import (
	"strings"
	"testing"
)

func day(date string, count int64, meters float64) histogramBucket {
	return histogramBucket{Date: date, Count: count, DistanceMeters: meters}
}

func TestStatsOf(t *testing.T) {
	days := []histogramBucket{
		day("2025-12-30", 1, 1000),
		day("2025-12-31", 2, 2000),
		day("2026-01-01", 1, 500), // a streak across the new year counts, as the SQL does
		day("2026-01-03", 1, 100),
		day("2026-03-01", 3, 4000),
		day("2026-03-02", 1, 10),
	}
	st := statsOf(days)
	if st.Count != 9 || st.DistanceMeters != 7610 || st.ActiveDays != 6 || st.LongestStreakDays != 3 {
		t.Errorf("all time: %+v", st)
	}
	if st := statsOf(daysInYear(days, 2026)); st.Count != 6 || st.ActiveDays != 4 || st.LongestStreakDays != 2 {
		t.Errorf("2026: %+v", st)
	}
	if st := statsOf(nil); st != (activityStatsBlock{}) {
		t.Errorf("no days: %+v", st)
	}
	if got := daysInYear(days, 2024); len(got) != 0 {
		t.Errorf("2024: %v", got)
	}
}

func TestBuildYearGrid(t *testing.T) {
	// 2026 starts on a Thursday: four padding days before Jan 1 in the first column.
	g := buildYearGrid(2026, []histogramBucket{day("2026-01-01", 2, 5000), day("2026-12-31", 1, 100)}, "count", false)
	if g.Weeks != 53 || len(g.Cells) != 53*7 {
		t.Fatalf("weeks %d, cells %d", g.Weeks, len(g.Cells))
	}
	for i := 0; i < 4; i++ {
		if !g.Cells[i].Pad {
			t.Errorf("cell %d should pad before Jan 1", i)
		}
	}
	if c := g.Cells[4]; c.Pad || c.Level != 2 || c.Title != "2026-01-01: 2 activities, 5.0 km" {
		t.Errorf("Jan 1: %+v", c)
	}
	if c := g.Cells[5]; c.Level != 0 || c.Title != "2026-01-02: no activity" {
		t.Errorf("Jan 2: %+v", c)
	}
	if len(g.Months) != 12 || g.Months[0] != (monthTick{"JAN", 1}) {
		t.Errorf("months: %v", g.Months)
	}
	if !strings.HasPrefix(g.Stats, "3 activities · 5.1 km · 2 active days · longest streak 1 day") {
		t.Errorf("stats line %q", g.Stats)
	}
}

func TestShading(t *testing.T) {
	for n, want := range map[int64]int{0: 0, 1: 1, 2: 2, 3: 3, 9: 3} {
		if got := shadeLevel(day("2026-01-01", n, 0), "count", 0, 0); got != want {
			t.Errorf("count %d: level %d, want %d", n, got, want)
		}
	}
	// The cut points are the sorted non-zero distances at floor(n/3) and floor(2n/3) — YearGrid.tsx's
	// rule, kept as it was: here 200 and 300, so only a day past 300 reaches the darkest shade.
	days := []histogramBucket{day("a", 1, 100), day("b", 1, 200), day("c", 1, 300), day("d", 1, 0)}
	t1, t2 := distanceThresholds(days)
	for m, want := range map[float64]int{0: 0, 100: 1, 200: 1, 250: 2, 300: 2, 301: 3} {
		if got := shadeLevel(day("x", 1, m), "distance", t1, t2); got != want {
			t.Errorf("%v m: level %d, want %d (thresholds %v, %v)", m, got, want, t1, t2)
		}
	}
}

func TestProfileHref(t *testing.T) {
	for _, tc := range [][3]string{
		{"count", "week", "/profile"},
		{"distance", "week", "/profile?shade=distance"},
		{"count", "month", "/profile?bucket=month"},
		{"distance", "month", "/profile?shade=distance&bucket=month"},
	} {
		if got := profileHref(tc[0], tc[1]); got != tc[2] {
			t.Errorf("profileHref(%s, %s) = %q, want %q", tc[0], tc[1], got, tc[2])
		}
	}
}

func TestTrendBars(t *testing.T) {
	bars := buildTrendBars([]trendPeriod{
		{PeriodStart: "2026-09-07", Count: 3, DistanceMeters: 10000, MovingSeconds: 7200, ElevationGainM: 120},
		{PeriodStart: "2026-09-14", Count: 0, DistanceMeters: 0},
	}, true)
	if bars[0].HeightPercent != 100 || bars[1].HeightPercent != 0 {
		t.Errorf("heights %v, %v", bars[0].HeightPercent, bars[1].HeightPercent)
	}
	if bars[0].Title != "Sep 7: 6.2 mi · 3 activities · 2 h moving · 394 ft gain" {
		t.Errorf("title %q", bars[0].Title)
	}
}
