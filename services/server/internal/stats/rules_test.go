package stats

import (
	"maps"
	"testing"
	"time"
)

func TestDayDue(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 30, 0, 0, time.UTC)
	yesterday := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) *time.Time { v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC); return &v }
	for _, tc := range []struct {
		name string
		last *time.Time
		now  time.Time
		due  bool
	}{
		{"nothing taken yet", nil, now, true},
		{"yesterday taken", day(2026, 10, 13), now, false},
		{"yesterday taken, late in the day", day(2026, 10, 13), now.Add(23 * time.Hour), false},
		{"the day before taken", day(2026, 10, 12), now, true},
		{"days missed", day(2026, 10, 1), now, true},
		{"now in another zone", day(2026, 10, 13), now.In(time.FixedZone("UTC-5", -5*3600)), false},
	} {
		got, due := dayDue(tc.last, tc.now)
		if !got.Equal(yesterday) || due != tc.due {
			t.Errorf("%s: got %v %v, want %v %v", tc.name, got, due, yesterday, tc.due)
		}
	}
}

func TestFoldSmallGroups(t *testing.T) {
	for _, tc := range []struct {
		name     string
		groups   map[string]float64
		accounts map[string]float64
		want     map[string]float64
	}{
		{"a group of 4 folds", map[string]float64{"de": 9}, map[string]float64{"de": 4}, map[string]float64{"other": 9}},
		{"a group of 5 stays", map[string]float64{"de": 9}, map[string]float64{"de": 5}, map[string]float64{"de": 9}},
		{
			"small groups add up into one other",
			map[string]float64{"de": 3, "fr": 2, "us": 40},
			map[string]float64{"de": 3, "fr": 2, "us": 40},
			map[string]float64{"other": 5, "us": 40},
		},
		{"nothing to fold, no other", map[string]float64{"us": 7}, map[string]float64{"us": 7}, map[string]float64{"us": 7}},
		{"value folds by accounts, not by itself", map[string]float64{"de": 300}, map[string]float64{"de": 2}, map[string]float64{"other": 300}},
	} {
		if got := foldSmallGroups(tc.groups, tc.accounts, minGroupAccounts); !maps.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKeyFor(t *testing.T) {
	for _, tc := range []struct{ prefix, group, want string }{
		{"accounts_by_country", "DE", "accounts_by_country.de"},
		{"accounts_by_country", "", "accounts_by_country.unset"},
		{"locale", "pt-BR", "locale.pt_br"},
		{"jobs", "render_fog.done", "jobs.render_fog.done"},
		{"jobs", "ingest.", "jobs.ingest.unset"},
		{"ingest_failed", "Bad File!", "ingest_failed.bad_file_"},
	} {
		if got := keyFor(tc.prefix, tc.group); got != tc.want {
			t.Errorf("keyFor(%q, %q) = %q, want %q", tc.prefix, tc.group, got, tc.want)
		}
	}
}

func TestParseTileKey(t *testing.T) {
	const u = "0b9a8c52-6f1e-4c4e-9a43-2f7a1c3e5d10"
	for _, tc := range []struct {
		key  string
		want tileKey
		ok   bool
	}{
		{"fog/" + u + "/14/8800/5370.png", tileKey{u, 14, 8800, 5370}, true},
		{"heatmap/" + u + "/0/0/0.png", tileKey{u, 0, 0, 0}, true},
		{"fog/" + u + "/14/8800/5370.webp", tileKey{}, false},
		{"fog/" + u + "/14/8800.png", tileKey{}, false},
		{"fog/not-a-user/14/1/2.png", tileKey{}, false},
		{"fog/" + u + "/14/-1/2.png", tileKey{}, false},
		{"raw/" + u + "/14/1/2.png", tileKey{}, false},
	} {
		got, ok := parseTileKey(tc.key)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseTileKey(%q) = %+v, %v; want %+v, %v", tc.key, got, ok, tc.want, tc.ok)
		}
	}
}
