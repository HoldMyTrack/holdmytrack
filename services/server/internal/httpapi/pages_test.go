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

func TestAuthPagesRenderSignedOut(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct {
		path string
		want []string
		not  []string
	}{
		{"/signin", []string{`action="/signin"`, `action="/demo"`, `href="/forgot"`, `href="/signup"`}, []string{"Continue with Google", `role="alert"`}},
		{"/signin?error=google", []string{"Couldn&#39;t sign in with Google. Please try again."}, nil},
		{"/signup", []string{`action="/signup"`, `name="timezone"`, "Intl.DateTimeFormat"}, []string{"Back to the map"}},
		{"/forgot", []string{`action="/forgot"`, "Send reset link"}, nil},
		{"/reset?token=abc", []string{`action="/reset"`, `name="token" value="abc"`}, nil},
		{"/reset", []string{"This reset link is invalid or has expired.", `href="/forgot"`}, []string{`action="/reset"`}},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, rec.Code)
		}
		body := rec.Body.String()
		for _, w := range tc.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", tc.path, w)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(body, n) {
				t.Errorf("%s: unexpected %q", tc.path, n)
			}
		}
	}
}

// A form the account core rejects before touching the database comes back as the same page,
// at the failure's status, with the message as a sentence and the email kept.
func TestAuthFormsRerenderOnValidationError(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct {
		path, body, msg string
	}{
		{"/signin", "email=not-an-email&password=long-enough", "Invalid email address."},
		{"/signup", "email=someone%40example.com&password=short", "Password must be at least 8 characters."},
		{"/forgot", "email=nope", "Invalid email address."},
		{"/reset", "token=abc&password=short", "Password must be at least 8 characters."},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://app.example")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.path, rec.Code)
			continue
		}
		if body := rec.Body.String(); !strings.Contains(body, tc.msg) {
			t.Errorf("%s: missing message %q", tc.path, tc.msg)
		}
	}
	// The email typed is kept on the re-rendered form.
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader("email=someone%40example.com&password=short"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `value="someone@example.com"`) {
		t.Errorf("signup: typed email not kept")
	}
}

func TestAuthFormsRequireSameOrigin(t *testing.T) {
	s := newPagesTestServer(t)
	for _, path := range []string{"/signin", "/signup", "/demo", "/forgot", "/reset", "/verify-pending/resend", "/verify-pending/email"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("email=a%40b.c&password=long-enough"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from a foreign origin: status %d, want 403", path, rec.Code)
		}
	}
}

func TestVerifyPendingSignedOutGoesToSignIn(t *testing.T) {
	s := newPagesTestServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/verify-pending", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/signin" {
		t.Errorf("status %d, location %q; want 303 to /signin", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"invalid email or password": "Invalid email or password.",
		"already a sentence.":       "Already a sentence.",
		"éclair":                    "Éclair.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSignInOffersGoogleOnlyWhenConfigured(t *testing.T) {
	pages, err := web.New(web.Embedded(), false, "test", "https://app.example")
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, slog.New(slog.DiscardHandler), nil, "https://app.example", "", "test", false,
		GoogleOAuthConfig{ClientID: testClientID, ClientSecret: "secret", RedirectURL: "https://app.example/v1/auth/google/callback"}, pages)
	for _, path := range []string{"/signin", "/signup"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		if !strings.Contains(body, `href="/v1/auth/google/start"`) || !strings.Contains(body, "Continue with Google") {
			t.Errorf("%s: no Google button with Google configured", path)
		}
		// The browser's timezone rides along for an account Google creates (FR-1.9).
		if !strings.Contains(body, "'?tz=' + encodeURIComponent") {
			t.Errorf("%s: Google link doesn't add ?tz=", path)
		}
	}
}
