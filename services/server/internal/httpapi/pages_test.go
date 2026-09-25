package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// newPagesTestServer builds a real Server (routes and all) with no database: every request
// below is signed out, and a signed-out page never touches the pool.
func newPagesTestServer(t *testing.T) *Server {
	t.Helper()
	pages, err := web.New(web.Embedded(), false, "test", "https://app.example")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	return New(nil, nil, slog.New(slog.DiscardHandler), nil, "https://app.example", "", "test", false, GoogleOAuthConfig{}, pages)
}

func TestPagesRenderSignedOut(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct {
		path, title string
		noIndex     bool
	}{
		{"/about", "About HoldMyTrack", false},
		{"/help", "Help — HoldMyTrack", true},
		{"/contacts", "Contacts — HoldMyTrack", false},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: content type %q", tc.path, ct)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: pages carry the account in their header and must not be cached", tc.path)
		}
		if !strings.Contains(body, "<title>"+tc.title) {
			t.Errorf("%s: missing title %q", tc.path, tc.title)
		}
		if !strings.Contains(body, `href="`+tc.path+`" aria-current="page"`) {
			t.Errorf("%s: Info menu doesn't mark the current page", tc.path)
		}
		// Signed out: a Sign in link, no account menu and no Sign out form.
		if !strings.Contains(body, ">Sign in</a>") || strings.Contains(body, `action="/logout"`) {
			t.Errorf("%s: signed-out header wrong", tc.path)
		}
		if hasNoIndex := strings.Contains(body, `name="robots" content="noindex"`); hasNoIndex != tc.noIndex {
			t.Errorf("%s: noindex = %v, want %v", tc.path, hasNoIndex, tc.noIndex)
		}
		if !tc.noIndex && !strings.Contains(body, `<link rel="canonical" href="https://app.example`+tc.path+`"`) {
			t.Errorf("%s: missing canonical link", tc.path)
		}
	}
}

func TestPagesStatic(t *testing.T) {
	s := newPagesTestServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/pages.css?v=test", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("pages.css: status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("pages.css should be cached long-term behind its ?v= buster")
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/static/ directory listing: status %d, want 404", rec.Code)
	}
}

func TestLogoutPageRequiresSameOrigin(t *testing.T) {
	s := newPagesTestServer(t)
	for name, tc := range map[string]struct {
		origin, referer string
		want            int
	}{
		"no origin or referer": {"", "", http.StatusForbidden},
		"foreign origin":       {"https://evil.example", "", http.StatusForbidden},
		"foreign referer":      {"", "https://evil.example/app.example", http.StatusForbidden},
		"lookalike referer":    {"", "https://app.example.evil.example/", http.StatusForbidden},
		"same origin":          {"https://app.example", "", http.StatusSeeOther},
		"same-origin referer":  {"", "https://app.example/about", http.StatusSeeOther},
	} {
		req := httptest.NewRequest(http.MethodPost, "/logout", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.referer != "" {
			req.Header.Set("Referer", tc.referer)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
			continue
		}
		if tc.want == http.StatusSeeOther {
			if loc := rec.Header().Get("Location"); loc != "/" {
				t.Errorf("%s: redirect to %q, want /", name, loc)
			}
			if c := rec.Header().Get("Set-Cookie"); !strings.Contains(c, sessionCookieName+"=;") || !strings.Contains(c, "Max-Age=0") {
				t.Errorf("%s: session cookie not cleared: %q", name, c)
			}
		}
	}
}
