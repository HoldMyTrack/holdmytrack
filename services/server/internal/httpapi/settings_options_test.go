package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func settingsOptionsFor(t *testing.T, info authInfo) settingsOptionsResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/account/settings/options", nil)
	r = r.WithContext(context.WithValue(r.Context(), authContextKey, info))
	w := httptest.NewRecorder()
	(&Server{}).handleSettingsOptions(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp settingsOptionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func findOption(opts []settingsOption, value string) (settingsOption, bool) {
	for _, o := range opts {
		if o.Value == value {
			return o, true
		}
	}
	return settingsOption{}, false
}

func TestSettingsOptionsNamesCountriesInTheAccountsLanguage(t *testing.T) {
	en := settingsOptionsFor(t, authInfo{timezone: "UTC", locale: "en"})
	ru := settingsOptionsFor(t, authInfo{timezone: "UTC", locale: "ru"})
	de, ok := findOption(en.Countries, "DE")
	if !ok || de.Label != "Germany" {
		t.Errorf("English DE = %+v, %v", de, ok)
	}
	deRU, ok := findOption(ru.Countries, "DE")
	if !ok || deRU.Label != "Германия" {
		t.Errorf("Russian DE = %+v, %v", deRU, ok)
	}
	if len(en.Languages) < 2 {
		t.Errorf("languages = %+v", en.Languages)
	}
}

func TestSettingsOptionsKeepsTheAccountsOwnTimezone(t *testing.T) {
	// A renamed zone comes back under its current name, and UTC is always offered.
	resp := settingsOptionsFor(t, authInfo{timezone: "Europe/Kiev", locale: "en"})
	var found, utc bool
	for _, g := range resp.Timezones {
		if _, ok := findOption(g.Options, "Europe/Kyiv"); ok {
			found = true
		}
		if _, ok := findOption(g.Options, "UTC"); ok {
			utc = true
		}
	}
	if !found || !utc {
		t.Errorf("Europe/Kyiv found %v, UTC found %v", found, utc)
	}
}

func TestSettingsOptionsNamesTimezoneRegions(t *testing.T) {
	for _, g := range settingsOptionsFor(t, authInfo{timezone: "UTC", locale: "ru"}).Timezones {
		if g.Region == "Europe" && g.Label != "Европа" {
			t.Errorf("Europe labelled %q", g.Label)
		}
	}
}
