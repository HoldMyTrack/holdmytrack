package web

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Country is one entry of the Settings page's Country list (places_data.go).
type Country struct {
	Code string
	Name string
}

// TimezoneOption is one entry of the Settings page's Timezone list: the IANA id the account
// stores, and a label to show — "New York · GMT−05:00".
type TimezoneOption struct {
	ID    string
	Label string
}

// TimezoneGroup is one <optgroup>: every zone under one top-level region ("America",
// "Europe", …), sorted by place so a native <select>'s type-to-find lands on a city by its
// name. The picker this replaced sorted by offset instead; with a plain <select>, typing
// "Ber" finding Berlin is worth more than the order.
type TimezoneGroup struct {
	Region  string
	Options []TimezoneOption
}

// CurrentTimezoneName is id under its current IANA name — Asia/Kolkata for Asia/Calcutta,
// Europe/Kyiv for Europe/Kiev (timezoneRenames) — or id itself when it hasn't been renamed.
// Browsers still report the old spellings (CLDR keeps them), so the server applies this to
// every zone it's given before storing it.
func CurrentTimezoneName(id string) string {
	if current, ok := timezoneRenames[id]; ok {
		return current
	}
	return id
}

// TimezoneGroups builds the Settings page's Timezone list as of `at` (offsets are today's, so
// a DST zone shows whichever side of its change `at` falls on). `current` — the account's
// saved zone, under its current name, so one saved before a rename is still the one
// selected — is added if the list doesn't have it, so a stale list never hides anyone's own
// setting — as is UTC, which the list lacks. A zone Go can't load is left out, except
// `current`, which is shown without an offset.
func TimezoneGroups(at time.Time, current string) []TimezoneGroup {
	current = CurrentTimezoneName(current)
	// UTC isn't in CLDR's list, but it's where an account lands when signup couldn't read the
	// browser's zone, and a reasonable deliberate choice too — so it's always offered.
	ids := append(append([]string(nil), TimezoneIDs...), "UTC")
	if current != "" && !slices.Contains(ids, current) {
		ids = append(ids, current)
	}
	byRegion := map[string][]TimezoneOption{}
	for _, id := range ids {
		region, place := splitZone(id)
		label := place
		if loc, err := time.LoadLocation(id); err == nil {
			_, offset := at.In(loc).Zone()
			label += " · " + formatOffset(offset)
		} else if id != current {
			continue
		}
		byRegion[region] = append(byRegion[region], TimezoneOption{ID: id, Label: label})
	}
	groups := make([]TimezoneGroup, 0, len(byRegion))
	for region, opts := range byRegion {
		sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
		groups = append(groups, TimezoneGroup{Region: region, Options: opts})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Region < groups[j].Region })
	return groups
}

// splitZone turns "America/Argentina/Buenos_Aires" into ("America", "Buenos Aires, Argentina")
// and "UTC" into ("Other", "UTC").
func splitZone(id string) (region, place string) {
	parts := strings.Split(strings.ReplaceAll(id, "_", " "), "/")
	if len(parts) == 1 {
		return "Other", parts[0]
	}
	place = parts[len(parts)-1]
	for i := len(parts) - 2; i >= 1; i-- {
		place += ", " + parts[i]
	}
	return parts[0], place
}

// formatOffset renders seconds east of GMT as "GMT+05:30" / "GMT−05:00" — a true minus sign,
// always two-digit hours, as the picker it replaced did.
func formatOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "−", -seconds
	}
	return fmt.Sprintf("GMT%s%02d:%02d", sign, seconds/3600, seconds%3600/60)
}
