package web

import (
	"math"
	"strconv"
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

// FormatBytes is a download's size: "840 KB", "12 MB", "1.4 GB" — decimal units, as browsers
// and file managers show them.
func FormatBytes(l *i18n.Localizer, n int64) string {
	switch {
	case n >= 1e9:
		return l.T("unit.size.gb", "v", l.Float(float64(n)/1e9, 1))
	case n >= 1e6:
		return l.T("unit.size.mb", "v", l.Int((n+5e5)/1e6))
	default:
		return l.T("unit.size.kb", "v", l.Int((n+999)/1000))
	}
}

// ShortDate is "Sep 8" / "8 сент." for a YYYY-MM-DD day.
func ShortDate(l *i18n.Localizer, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return l.T("date.short", "day", strconv.Itoa(t.Day()), "month", l.T("month.short."+strconv.Itoa(int(t.Month()))))
}

// ShortDateYear is "Sep 8, 2026" / "8 сент. 2026 г." for a YYYY-MM-DD day.
func ShortDateYear(l *i18n.Localizer, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return l.T("date.short_year", "day", strconv.Itoa(t.Day()), "month", l.T("month.short."+strconv.Itoa(int(t.Month()))), "year", strconv.Itoa(t.Year()))
}

// MonthYear is "Sep 2026" / "сент. 2026 г." for a YYYY-MM-DD day's month.
func MonthYear(l *i18n.Localizer, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return l.T("date.month_year", "month", l.T("month.standalone."+strconv.Itoa(int(t.Month()))), "year", strconv.Itoa(t.Year()))
}
