package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The Profile page (ADR-0012, IMPLEMENTATION.md §4.8, docs/SPEC.md FR-7 and FR-9): the
// all-time stat cards, one activity grid per year back to the account's first activity, and
// the Trends chart — rendered on the server, no script. The shading toggle and the Trends
// week/month switch are links (`?shade=`, `?bucket=`), and a cell's or a bar's detail is its
// native tooltip (`title`).
//
// One query covers every grid: the daily totals from the first activity's year to now
// (dailyTotals, the histogram endpoint's own query). Each year's stats and the all-time cards
// are derived from those days here, rather than asked for one year at a time — count and
// distance are sums, active days the number of days, the longest streak the longest run of
// consecutive ones — so the page costs three queries however many years it shows: that,
// the earliest activity, and Trends.

// profileView is what templates/pages/profile.html reads from PageData.Page.
type profileView struct {
	Cards     []statCard
	Years     []yearGrid
	ShadeBy   string // "count" or "distance"
	Bucket    string // "week" or "month"
	ShadeHref map[string]string
	BucketURL map[string]string
	Trends    []trendBar
	TrendFrom string
	TrendTo   string
	// Legend is the grid legend's own words: "Shaded by activity count: less".
	Legend string
}

type statCard struct{ Label, Value string }

type yearGrid struct {
	Year  int
	Stats string
	Weeks int
	// Months are the column each month's label sits over, 1-based for grid-column-start.
	Months []monthTick
	// Cells run column by column, Sunday to Saturday, as the grid lays them out.
	Cells []gridCell
}

type monthTick struct {
	Label string
	Col   int
}

type gridCell struct {
	Pad   bool
	Level int
	Title string
}

type trendBar struct {
	HeightPercent float64
	Title         string
}

// GET /profile.
func (s *Server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	if home := acct.home(); home != "/" {
		http.Redirect(w, r, home, http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	shade := "count"
	if q.Get("shade") == "distance" {
		shade = "distance"
	}
	bucket := "week"
	if q.Get("bucket") == "month" {
		bucket = "month"
	}

	lang := pageLang(acct, r)
	l := i18n.Get(lang)
	view, err := s.buildProfile(r.Context(), l, acct, shade, bucket, time.Now())
	if err != nil {
		s.log.Error("profile page failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.pages.Render(w, http.StatusOK, "profile", web.PageData{Title: l.T("meta.profile_title"), Path: "/profile", NoIndex: true, User: acct.user, Page: view, Lang: lang})
}

func (s *Server) buildProfile(ctx context.Context, l *i18n.Localizer, acct *pageAccount, shade, bucket string, now time.Time) (profileView, error) {
	userID, tz := acct.info.userID, acct.info.timezone
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	imperial := web.Imperial(acct.profile.Country)
	now = now.In(loc)
	currentYear := now.Year()

	firstYear := currentYear
	earliest, err := s.earliestActivity(ctx, userID)
	if err != nil {
		return profileView{}, err
	}
	if earliest != nil && earliest.In(loc).Year() < currentYear {
		firstYear = earliest.In(loc).Year()
	}
	days, err := s.dailyTotals(ctx, userID,
		time.Date(firstYear, time.January, 1, 0, 0, 0, 0, loc),
		time.Date(currentYear+1, time.January, 1, 0, 0, 0, 0, loc), tz)
	if err != nil {
		return profileView{}, err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	periods, err := s.activityTrends(ctx, userID, bucket, today.AddDate(0, -histogramWindowMonths, 0), today.AddDate(0, 0, 1), tz)
	if err != nil {
		return profileView{}, err
	}

	all := statsOf(days)
	view := profileView{
		Cards: []statCard{
			{l.T("profile.card_activities"), l.Int(all.Count)},
			{l.T("profile.card_distance"), web.FormatTotalDistance(l, all.DistanceMeters, imperial)},
			{l.T("profile.card_active_days"), l.Int(all.ActiveDays)},
			{l.T("profile.card_longest_streak"), l.N("count.days", all.LongestStreakDays)},
		},
		ShadeBy:   shade,
		Bucket:    bucket,
		ShadeHref: map[string]string{"count": profileHref("count", bucket), "distance": profileHref("distance", bucket)},
		BucketURL: map[string]string{"week": profileHref(shade, "week"), "month": profileHref(shade, "month")},
		Legend:    l.T("profile.legend_" + shade),
	}
	for year := currentYear; year >= firstYear; year-- {
		view.Years = append(view.Years, buildYearGrid(l, year, daysInYear(days, year), shade, imperial))
	}
	view.Trends = buildTrendBars(l, periods, imperial)
	if len(periods) > 0 {
		view.TrendFrom, view.TrendTo = web.ShortDate(l, periods[0].PeriodStart), web.ShortDate(l, periods[len(periods)-1].PeriodStart)
	}
	return view, nil
}

// profileHref keeps the other toggle's state when one changes. The defaults are left out, so
// the plain /profile is the default view.
func profileHref(shade, bucket string) string {
	href, sep := "/profile", "?"
	if shade != "count" {
		href += sep + "shade=" + shade
		sep = "&"
	}
	if bucket != "week" {
		href += sep + "bucket=" + bucket
	}
	return href
}

// statsOf is activityStats computed from daily totals instead of in SQL: the same four
// numbers, since a day's bucket is exactly the day activityStats' queries group by.
func statsOf(days []histogramBucket) activityStatsBlock {
	var b activityStatsBlock
	var run int64
	var prev time.Time
	for i, d := range days {
		b.Count += d.Count
		b.DistanceMeters += d.DistanceMeters
		b.ActiveDays++
		day, _ := time.Parse(dateLayout, d.Date)
		if i > 0 && day.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > b.LongestStreakDays {
			b.LongestStreakDays = run
		}
		prev = day
	}
	return b
}

// daysInYear is the slice of days (ascending, YYYY-MM-DD) that fall in year.
func daysInYear(days []histogramBucket, year int) []histogramBucket {
	prefix := fmt.Sprintf("%04d-", year)
	lo := sort.Search(len(days), func(i int) bool { return days[i].Date >= prefix })
	hi := sort.Search(len(days), func(i int) bool { return days[i].Date >= fmt.Sprintf("%04d-", year+1) })
	return days[lo:hi]
}

// buildYearGrid lays one year out as whole weeks, Sunday to Saturday — so the first and last
// columns carry padding days outside the year — with each day's shading level and tooltip.
// Dates are calendar days, laid out in UTC arithmetic: the buckets are already the account's
// own local days.
func buildYearGrid(l *i18n.Localizer, year int, days []histogramBucket, shade string, imperial bool) yearGrid {
	byDate := make(map[string]histogramBucket, len(days))
	for _, d := range days {
		byDate[d.Date] = d
	}
	jan1 := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	dec31 := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
	gridStart := jan1.AddDate(0, 0, -int(jan1.Weekday()))
	weeks := (int(dec31.Sub(gridStart).Hours()/24) + 1 + 6) / 7
	t1, t2 := distanceThresholds(days)

	g := yearGrid{Year: year, Weeks: weeks, Stats: yearStatsLine(l, statsOf(days), imperial)}
	lastCol := -1
	for col := 0; col < weeks; col++ {
		for row := 0; row < 7; row++ {
			day := gridStart.AddDate(0, 0, col*7+row)
			if day.Before(jan1) || day.After(dec31) {
				g.Cells = append(g.Cells, gridCell{Pad: true})
				continue
			}
			if day.Day() == 1 && col != lastCol {
				g.Months = append(g.Months, monthTick{Label: strings.ToUpper(l.T(fmt.Sprintf("month.standalone.%d", day.Month()))), Col: col + 1})
				lastCol = col
			}
			date := day.Format(dateLayout)
			b := byDate[date]
			cell := gridCell{Level: shadeLevel(b, shade, t1, t2), Title: l.T("profile.cell_empty", "date", date)}
			if b.Count > 0 {
				cell.Title = l.T("profile.cell", "date", date, "activities", l.N("count.activities", b.Count), "distance", web.FormatDistance(l, b.DistanceMeters, imperial))
			}
			g.Cells = append(g.Cells, cell)
		}
	}
	return g
}

// distanceThresholds splits a year's non-zero day distances into thirds, so distance shading
// is relative to that year's own spread (YearGrid.tsx's rule): the 1/3 and 2/3 points.
func distanceThresholds(days []histogramBucket) (float64, float64) {
	var ds []float64
	for _, d := range days {
		if d.DistanceMeters > 0 {
			ds = append(ds, d.DistanceMeters)
		}
	}
	if len(ds) == 0 {
		return 0, 0
	}
	sort.Float64s(ds)
	at := func(p float64) float64 { return ds[min(len(ds)-1, int(math.Floor(p*float64(len(ds)))))] }
	return at(1.0 / 3), at(2.0 / 3)
}

// shadeLevel is 0–3: by count, 1, 2, and 3-or-more activities; by distance, the year's thirds.
func shadeLevel(b histogramBucket, shade string, t1, t2 float64) int {
	if shade == "count" {
		return int(min(b.Count, 3))
	}
	switch {
	case b.DistanceMeters <= 0:
		return 0
	case b.DistanceMeters <= t1:
		return 1
	case b.DistanceMeters <= t2:
		return 2
	}
	return 3
}

func yearStatsLine(l *i18n.Localizer, st activityStatsBlock, imperial bool) string {
	return l.T("profile.year_stats",
		"activities", l.N("count.activities", st.Count),
		"distance", web.FormatTotalDistance(l, st.DistanceMeters, imperial),
		"active_days", l.N("count.active_days", st.ActiveDays),
		"streak", l.N("count.days", st.LongestStreakDays))
}

// buildTrendBars scales each period's bar on a log curve against the busiest period
// (Trends.tsx's rule): one huge week would otherwise flatten every other bar to nothing.
func buildTrendBars(l *i18n.Localizer, periods []trendPeriod, imperial bool) []trendBar {
	var peak float64
	for _, p := range periods {
		peak = math.Max(peak, p.DistanceMeters)
	}
	bars := make([]trendBar, 0, len(periods))
	for _, p := range periods {
		h := 0.0
		if peak > 0 {
			h = math.Log(p.DistanceMeters+1) / math.Log(peak+1) * 100
		}
		bars = append(bars, trendBar{
			HeightPercent: math.Round(h*10) / 10,
			Title: l.T("profile.trend_bar",
				"date", web.ShortDate(l, p.PeriodStart), "distance", web.FormatTotalDistance(l, p.DistanceMeters, imperial),
				"activities", l.N("count.activities", p.Count), "hours", web.FormatHours(l, p.MovingSeconds),
				"gain", web.FormatElevation(l, p.ElevationGainM, imperial)),
		})
	}
	return bars
}
