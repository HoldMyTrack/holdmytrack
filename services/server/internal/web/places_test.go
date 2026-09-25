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
		// IANA's current names, not CLDR's older spellings (Asia/Calcutta, Europe/Kiev).
		"Asia/Kolkata":                   {"Asia", "Kolkata · GMT+05:30"},
		"Europe/Kyiv":                    {"Europe", "Kyiv · GMT+02:00"},
		"America/Argentina/Buenos_Aires": {"America", "Buenos Aires, Argentina · GMT−03:00"},
		"UTC":                            {"Other", "UTC · GMT+00:00"},
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
	for _, old := range []string{"Asia/Calcutta", "Europe/Kiev", "America/Buenos_Aires", "Pacific/Truk"} {
		if _, _, ok := find(groups, old); ok {
			t.Errorf("the list still offers the old spelling %s", old)
		}
	}
	// A place merged into a neighbour's clocks keeps its own name — merges aren't renames.
	if _, _, ok := find(groups, "Europe/Vaduz"); !ok {
		t.Errorf("Europe/Vaduz is missing")
	}
	// An account's saved zone the list lacks is still offered.
	if _, _, ok := find(TimezoneGroups(winter, "US/Eastern"), "US/Eastern"); !ok {
		t.Errorf("a saved zone missing from the list isn't offered")
	}
	// One saved under an old spelling is offered — and so selected — under its current name,
	// not added as a second entry.
	withOld := TimezoneGroups(winter, "Asia/Calcutta")
	if _, _, ok := find(withOld, "Asia/Calcutta"); ok {
		t.Errorf("a saved old spelling was added to the list")
	}
}

func TestCurrentTimezoneName(t *testing.T) {
	for in, want := range map[string]string{
		"Asia/Calcutta":        "Asia/Kolkata",
		"Europe/Kiev":          "Europe/Kyiv",
		"America/Buenos_Aires": "America/Argentina/Buenos_Aires",
		"Pacific/Truk":         "Pacific/Chuuk", // the #= target, not Port Moresby
		"Europe/Vaduz":         "Europe/Vaduz",  // a merge, not a rename
		"Europe/Berlin":        "Europe/Berlin",
	} {
		if got := CurrentTimezoneName(in); got != want {
			t.Errorf("CurrentTimezoneName(%q) = %q, want %q", in, got, want)
		}
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
