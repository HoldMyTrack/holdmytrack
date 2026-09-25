package web

import "testing"

func TestFormat(t *testing.T) {
	for got, want := range map[string]string{
		FormatDistance(12345, false):        "12.3 km",
		FormatDistance(12345, true):         "7.7 mi",
		FormatTotalDistance(8400, false):    "8.4 km",
		FormatTotalDistance(8000, false):    "8 km",
		FormatTotalDistance(1234567, false): "1,235 km",
		FormatElevation(1203.4, false):      "1,203 m",
		FormatElevation(1000, true):         "3,281 ft",
		FormatHours(5400):                   "2",
		FormatInt(1234567):                  "1,234,567",
		FormatInt(-1234):                    "-1,234",
		FormatInt(12):                       "12",
		Plural(1, "day", "days"):            "1 day",
		Plural(1500, "day", "days"):         "1,500 days",
		ShortDate("2026-09-08"):             "Sep 8",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if !Imperial("US") || Imperial("DE") || Imperial("") {
		t.Errorf("Imperial")
	}
}
