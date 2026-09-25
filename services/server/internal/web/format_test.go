package web

import (
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

func TestFormat(t *testing.T) {
	en, ru := i18n.Get("en"), i18n.Get("ru")
	for got, want := range map[string]string{
		FormatDistance(en, 12345, false):        "12.3 km",
		FormatDistance(en, 12345, true):         "7.7 mi",
		FormatTotalDistance(en, 8400, false):    "8.4 km",
		FormatTotalDistance(en, 8000, false):    "8 km",
		FormatTotalDistance(en, 8960, false):    "9 km",
		FormatTotalDistance(en, 1234567, false): "1,235 km",
		FormatElevation(en, 1203.4, false):      "1,203 m",
		FormatElevation(en, 1000, true):         "3,281 ft",
		FormatHours(en, 5400):                   "2",
		ShortDate(en, "2026-09-08"):             "Sep 8",
		FormatDistance(ru, 12345, false):        "12,3 км",
		FormatDistance(ru, 12345, true):         "7,7 ми",
		FormatTotalDistance(ru, 8400, false):    "8,4 км",
		FormatTotalDistance(ru, 1234567, false): "1\u00a0235 км",
		FormatElevation(ru, 1000, true):         "3\u00a0281 фт",
		ShortDate(ru, "2026-09-08"):             "8 сент.",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if !Imperial("US") || Imperial("DE") || Imperial("") {
		t.Errorf("Imperial")
	}
}

func TestCountriesIn(t *testing.T) {
	if len(CountriesIn("ru")) != len(Countries) {
		t.Fatalf("ru has %d countries, en %d", len(CountriesIn("ru")), len(Countries))
	}
	names := map[string]string{}
	for _, c := range CountriesIn("ru") {
		names[c.Code] = c.Name
	}
	for _, c := range Countries {
		if names[c.Code] == "" {
			t.Errorf("%s has no Russian name", c.Code)
		}
	}
	if names["DE"] != "Германия" || CountriesIn("xx")[0] != Countries[0] {
		t.Errorf("DE = %q", names["DE"])
	}
}
