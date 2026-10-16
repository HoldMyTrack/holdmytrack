package httpapi

// Covers docs/SPEC.md §1.5 (old Android builds refused).

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAndroidBuild(t *testing.T) {
	for _, tc := range []struct {
		ua      string
		build   int
		android bool
	}{
		{"HoldMyTrack-Android/0.14 (62da50b; 812)", 812, true},
		{"HoldMyTrack-Android/0.13 (62da50b)", 0, true},
		{"HoldMyTrack-Android/0.13", 0, true},
		{"HoldMyTrack-Android/0.14 (62da50b; x)", 0, true},
		{"HoldMyTrack-Android/0.14 (62da50b; -3)", 0, true},
		{"Mozilla/5.0 (Linux; Android 16; Pixel 10a) AppleWebKit/537.36", 0, false},
		{"", 0, false},
	} {
		build, android := androidBuild(tc.ua)
		if build != tc.build || android != tc.android {
			t.Errorf("androidBuild(%q) = %d, %v, want %d, %v", tc.ua, build, android, tc.build, tc.android)
		}
	}
}

func TestAppOutdated(t *testing.T) {
	for _, tc := range []struct {
		ua   string
		min  int
		want bool
	}{
		{"HoldMyTrack-Android/0.14 (62da50b; 812)", 0, false},
		{"HoldMyTrack-Android/0.14 (62da50b; 812)", 812, false},
		{"HoldMyTrack-Android/0.14 (62da50b; 812)", 813, true},
		{"HoldMyTrack-Android/0.13 (62da50b)", 1, true},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", 813, false},
		{"", 813, false},
	} {
		if got := appOutdated(tc.ua, tc.min); got != tc.want {
			t.Errorf("appOutdated(%q, %d) = %v, want %v", tc.ua, tc.min, got, tc.want)
		}
	}
}

func TestServeHTTPRefusesAnOutdatedAppOnV1Only(t *testing.T) {
	s := &Server{log: slog.New(slog.DiscardHandler), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/test", func(w http.ResponseWriter, r *http.Request) {})
	s.mux.HandleFunc("GET /tiles/v1/test", func(w http.ResponseWriter, r *http.Request) {})
	s.mux.HandleFunc("GET /help", func(w http.ResponseWriter, r *http.Request) {})
	s.SetMinAndroidBuild(813)

	get := func(path, ua string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}
	old := "HoldMyTrack-Android/0.14 (62da50b; 812)"
	w := get("/v1/test", old)
	var body apiErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusUpgradeRequired || body.Error != "app_outdated" || body.Message == "" {
		t.Errorf("an old build on /v1/ got %d %+v, want 426 app_outdated with a message", w.Code, body)
	}
	for _, tc := range []struct{ path, ua string }{
		{"/v1/test", "HoldMyTrack-Android/0.14 (62da50b; 813)"},
		{"/v1/test", "Mozilla/5.0"},
		{"/tiles/v1/test", old},
		{"/help", old},
	} {
		if w := get(tc.path, tc.ua); w.Code != http.StatusOK {
			t.Errorf("%s from %q got %d, want 200", tc.path, tc.ua, w.Code)
		}
	}
}
