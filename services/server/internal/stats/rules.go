package stats

import (
	"strings"
	"time"
)

// minGroupAccounts is the fewest accounts a grouped value may stand for before it's folded
// into its stat's "other" group (foldSmallGroups), so no stored value describes one person.
const minGroupAccounts = 5

// dayDue reports which UTC day the snapshot is for at now (the day that just ended) and whether
// it is still owed, given the newest day already taken (nil for none). A missed day isn't
// backfilled: its state values can't be read after the fact.
func dayDue(lastDay *time.Time, now time.Time) (day time.Time, due bool) {
	y, m, d := now.UTC().Date()
	day = time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return day, lastDay == nil || lastDay.Before(day)
}

// foldSmallGroups merges every group with fewer than min accounts behind it into "other".
// accounts holds, per group, how many accounts its value stands for; a group missing from it
// counts as none. With nothing to fold there is no "other" key.
func foldSmallGroups(groups map[string]float64, accounts map[string]float64, min int) map[string]float64 {
	out := make(map[string]float64, len(groups))
	for g, v := range groups {
		if accounts[g] < float64(min) {
			out["other"] += v
		} else {
			out[g] += v
		}
	}
	return out
}

// keyFor is the key a grouped stat stores its group under: prefix, a dot and groupName(group).
func keyFor(prefix, group string) string {
	return prefix + "." + groupName(group)
}

// groupName is a group as a key-safe name: lower case, with anything outside [a-z0-9_] made "_",
// except the dots that split a group of more than one part ("ingest.done"). An empty group or
// part is "unset".
func groupName(group string) string {
	parts := strings.Split(strings.ToLower(group), ".")
	for i, part := range parts {
		if part == "" {
			parts[i] = "unset"
			continue
		}
		b := []byte(part)
		for j, c := range b {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				b[j] = '_'
			}
		}
		parts[i] = string(b)
	}
	return strings.Join(parts, ".")
}
