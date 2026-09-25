package web

import (
	"strings"
	"testing"
	"time"
)

func TestSplitZone(t *testing.T) {
	for id, want := range map[string][2]string{
		"America/Argentina/Buenos_Aires": {"America", "Buenos Aires, Argentina"},
		"UTC":                            {"Other", "UTC"},
		"Europe/Kiev":                    {"Europe", "Kiev"},
	} {
		if region, place := splitZone(id); region != want[0] || place != want[1] {
			t.Errorf("splitZone(%q) = %q, %q; want %q, %q", id, region, place, want[0], want[1])
		}
	}
}

func TestTimezoneGroups(t *testing.T) {
	winter := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	groups := TimezoneGroups(winter, "")
	find := func(groups []TimezoneGroup, id string) (string, TimezoneOption, bool) {
		for _, g := range groups {
			for _, o := range g.Options {
				if o.ID == id {
					return g.Region, o, true
				}
			}
		}
		return "", TimezoneOption{}, false
	}
	for id, want := range map[string]struct{ region, label string }{
		"America/New_York": {"America", "New York · GMT−05:00"},
		// CLDR's canonical ids, which keep some older spellings (not Asia/Kolkata).
		"Asia/Calcutta":        {"Asia", "Calcutta · GMT+05:30"},
		"America/Buenos_Aires": {"America", "Buenos Aires · GMT−03:00"},
		"UTC":                  {"Other", "UTC · GMT+00:00"},
	} {
		region, opt, ok := find(groups, id)
		if !ok || region != want.region || opt.Label != want.label {
			t.Errorf("%s: got %q in %q, want %q in %q", id, opt.Label, region, want.label, want.region)
		}
	}
	// Offsets are the given day's: New York is on daylight time in July.
	if _, opt, _ := find(TimezoneGroups(time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC), ""), "America/New_York"); !strings.HasSuffix(opt.Label, "GMT−04:00") {
		t.Errorf("July New York label %q", opt.Label)
	}
	// Sorted by region, then by place within one.
	for i := 1; i < len(groups); i++ {
		if groups[i-1].Region >= groups[i].Region {
			t.Fatalf("regions out of order: %q before %q", groups[i-1].Region, groups[i].Region)
		}
	}
	// An account's saved zone the list lacks is still offered.
	if _, _, ok := find(TimezoneGroups(winter, "US/Eastern"), "US/Eastern"); !ok {
		t.Errorf("a saved zone missing from the list isn't offered")
	}
}

func TestCountriesAreCodesAndNames(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Countries {
		if len(c.Code) != 2 || strings.ToUpper(c.Code) != c.Code || c.Name == "" || seen[c.Code] {
			t.Fatalf("bad or duplicate entry %+v", c)
		}
		seen[c.Code] = true
	}
	if !seen["US"] || !seen["DE"] {
		t.Errorf("list is missing obvious countries")
	}
}
