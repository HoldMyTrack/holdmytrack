package web

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
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

// Unit labels and number separators come from the page's language (l): "12.3 km" in English,
// "12,3 км" in Russian.

// FormatDistance is one day's or one activity's distance, one decimal: "12.3 km" / "7.6 mi".
func FormatDistance(l *i18n.Localizer, meters float64, imperial bool) string {
	if imperial {
		return l.T("unit.distance.mi", "v", l.Float(meters/metersPerMile, 1))
	}
	return l.T("unit.distance.km", "v", l.Float(meters/1000, 1))
}

// FormatTotalDistance is a sum over many activities: one decimal below 10, whole numbers
// with thousands separators above — "8.4 km", "1,234 km".
func FormatTotalDistance(l *i18n.Localizer, meters float64, imperial bool) string {
	v, key := meters/1000, "unit.distance.km"
	if imperial {
		v, key = meters/metersPerMile, "unit.distance.mi"
	}
	var s string
	if v < 10 {
		// toLocaleString's maximumFractionDigits: 1 drops a trailing ".0".
		if s = l.Float(v, 1); math.Round(v*10) == math.Round(v)*10 {
			s = l.Float(v, 0)
		}
	} else {
		s = l.Int(int64(math.Round(v)))
	}
	return l.T(key, "v", s)
}

// FormatElevation is a whole number of meters or feet: "1,203 m" / "3,947 ft".
func FormatElevation(l *i18n.Localizer, meters float64, imperial bool) string {
	if imperial {
		return l.T("unit.elevation.ft", "v", l.Int(int64(math.Round(meters/metersPerFoot))))
	}
	return l.T("unit.elevation.m", "v", l.Int(int64(math.Round(meters))))
}

// FormatHours is a whole number of hours: "12".
func FormatHours(l *i18n.Localizer, seconds int64) string {
	return l.Int(int64(math.Round(float64(seconds) / 3600)))
}

// LocalTime is the wall-clock date and time at `at` in zone: "Sep 28, 14:05" / "28 сент.,
// 14:05" — what Settings' Timezone field shows under itself, and the same shape the page's
// script renders with Intl.DateTimeFormat. "" for a zone Go can't load.
func LocalTime(l *i18n.Localizer, at time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		return ""
	}
	t := at.In(loc)
	return ShortDate(l, t.Format("2006-01-02")) + ", " + t.Format("15:04")
}

// ShortDate is "Sep 8" / "8 сент." for a YYYY-MM-DD day.
func ShortDate(l *i18n.Localizer, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return l.T("date.short", "day", strconv.Itoa(t.Day()), "month", l.T("month.short."+strconv.Itoa(int(t.Month()))))
}

// FormatDuration is "2h 15m" / "2 ч 15 мин", minutes below an hour and seconds below a
// minute — format.ts's formatDuration.
func FormatDuration(l *i18n.Localizer, seconds int64) string {
	h, m := seconds/3600, seconds%3600/60
	switch {
	case h > 0:
		return l.T("duration.hm", "h", strconv.FormatInt(h, 10), "m", strconv.FormatInt(m, 10))
	case m > 0:
		return l.T("duration.m", "m", strconv.FormatInt(m, 10))
	}
	return l.T("duration.s", "s", strconv.FormatInt(seconds, 10))
}

// ActivityType is an activity_type's display name: the catalog's for a common type, else the
// raw value with underscores spaced out and each word capitalized — format.ts's
// formatActivityType, which explains why that isn't a mapping to a fixed vocabulary.
func ActivityType(l *i18n.Localizer, activityType string) string {
	key := "activity_type." + strings.ToLower(activityType)
	if msg := l.T(key); msg != key {
		return msg
	}
	words := strings.FieldsFunc(activityType, func(r rune) bool { return r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
