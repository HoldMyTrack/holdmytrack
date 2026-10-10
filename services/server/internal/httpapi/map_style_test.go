package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMapStyleLanguage: ?lang= picks the labels' language on its primary subtag, and anything
// unsupported, or none, is English.
func TestMapStyleLanguage(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct{ query, want string }{
		{"", "en"},
		{"?lang=ru", "ru"},
		{"?lang=ru-RU", "ru"},
		{"?lang=RU", "ru"},
		{"?lang=de", "en"},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/map/style/light"+tc.query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status %d", tc.query, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"name:`+tc.want+`"`) {
			t.Errorf("%q: labels aren't name:%s", tc.query, tc.want)
		}
		// A Russian document falls back to name:en too, so English is told apart by this.
		if tc.want == "en" && strings.Contains(rec.Body.String(), `"name:ru"`) {
			t.Errorf("%q: English document reads name:ru", tc.query)
		}
	}
}
