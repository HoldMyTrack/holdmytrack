package web

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Units: the distance/elevation formatting the server-rendered pages need, ported from
// apps/web/src/ui/format.ts and units.ts so a number reads the same on a page as in the map
// app. Keep the two in step.

// Imperial is the account's unit system: miles and feet for the three countries where
// everyday distance is customarily miles (units.ts's IMPERIAL_COUNTRIES), km and meters for
// everyone else, and for an account with no country yet.
func Imperial(country string) bool {
	return country == "US" || country == "LR" || country == "MM"
}

const metersPerMile = 1609.344
const metersPerFoot = 0.3048

// FormatDistance is one day's or one activity's distance, one decimal: "12.3 km" / "7.6 mi".
func FormatDistance(meters float64, imperial bool) string {
	if imperial {
		return strconv.FormatFloat(meters/metersPerMile, 'f', 1, 64) + " mi"
	}
	return strconv.FormatFloat(meters/1000, 'f', 1, 64) + " km"
}

// FormatTotalDistance is a sum over many activities: one decimal below 10, whole numbers
// with thousands separators above — "8.4 km", "1,234 km".
func FormatTotalDistance(meters float64, imperial bool) string {
	v, unit := meters/1000, " km"
	if imperial {
		v, unit = meters/metersPerMile, " mi"
	}
	if v < 10 {
		// toLocaleString's maximumFractionDigits: 1 drops a trailing ".0".
		return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0") + unit
	}
	return FormatInt(int64(math.Round(v))) + unit
}

// FormatElevation is a whole number of meters or feet: "1,203 m" / "3,947 ft".
func FormatElevation(meters float64, imperial bool) string {
	if imperial {
		return FormatInt(int64(math.Round(meters/metersPerFoot))) + " ft"
	}
	return FormatInt(int64(math.Round(meters))) + " m"
}

// FormatHours is a whole number of hours: "12".
func FormatHours(seconds int64) string {
	return FormatInt(int64(math.Round(float64(seconds) / 3600)))
}

// FormatInt adds thousands separators: 1234567 → "1,234,567".
func FormatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Plural is "1 activity" / "3 activities": n with thousands separators and the right noun.
func Plural(n int64, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return FormatInt(n) + " " + many
}

// ShortDate is "Sep 8" for a YYYY-MM-DD day.
func ShortDate(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return t.Format("Jan 2")
}
