package stats

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	d := func(s string) *time.Time { v, _ := time.Parse(time.DateOnly, s); return &v }
	for _, tc := range []struct {
		args []string
		want request
		bad  bool
	}{
		{args: nil, want: request{}},
		{args: []string{"2026-10-01"}, want: request{day: d("2026-10-01")}},
		{args: []string{"active_30d"}, want: request{key: "active_30d", days: defaultDays}},
		{args: []string{"activities_by_source", "--days", "90"}, want: request{key: "activities_by_source", days: 90}},
		{args: []string{"--take"}, want: request{day: d("2026-10-13"), take: true}},
		{args: []string{"--take", "2026-10-01"}, want: request{day: d("2026-10-01"), take: true}},
		{args: []string{"--take", "yesterday"}, bad: true},
		{args: []string{"accounts", "--days", "0"}, bad: true},
		{args: []string{"accounts", "--weeks", "2"}, bad: true},
		{args: []string{"accounts", "--days"}, bad: true},
		{args: []string{"2026-10-01", "--days", "3"}, bad: true},
		{args: []string{"--csv"}, bad: true},
	} {
		got, err := parseArgs(tc.args, now)
		if tc.bad {
			if !errors.Is(err, ErrUsage) {
				t.Errorf("%v: err %v, want ErrUsage", tc.args, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		sameDay := (got.day == nil) == (tc.want.day == nil) && (got.day == nil || got.day.Equal(*tc.want.day))
		if !sameDay || got.key != tc.want.key || got.days != tc.want.days || got.take != tc.want.take {
			t.Errorf("%v: got %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

func TestFormatValue(t *testing.T) {
	for v, want := range map[float64]string{0: "0", 12: "12", 1381428: "1381428", 3.5: "3.50", 2.126: "2.13"} {
		if got := formatValue(v); got != want {
			t.Errorf("formatValue(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestSeriesTable(t *testing.T) {
	d1 := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(0, 0, 1)
	header, cells := seriesTable([]row{
		{d2, "activities_by_source.upload", 10},
		{d1, "activities_by_source.upload", 8},
		{d2, "activities_by_source.healthconnect", 2},
	})
	if want := []string{"day", "activities_by_source.healthconnect", "activities_by_source.upload"}; !slices.Equal(header, want) {
		t.Errorf("header %v, want %v", header, want)
	}
	want := [][]string{{"2026-10-12", "-", "8"}, {"2026-10-13", "2", "10"}}
	if !slices.EqualFunc(cells, want, slices.Equal) {
		t.Errorf("cells %v, want %v", cells, want)
	}
}

func TestDayTable(t *testing.T) {
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	_, cells := dayTable([]row{{day, "signups", 2}, {day, "accounts", 30}})
	want := [][]string{{"accounts", "30"}, {"signups", "2"}}
	if !slices.EqualFunc(cells, want, slices.Equal) {
		t.Errorf("cells %v, want %v", cells, want)
	}
}
