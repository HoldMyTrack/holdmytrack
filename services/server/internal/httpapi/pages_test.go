package httpapi

import (
	"encoding/json"
	"errors"
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
		{"/help", "Help — HoldMyTrack", false},
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
		// Signed out, these pages are the same for everyone, so they're cacheable — but only
		// for a request without a session cookie.
		if rec.Header().Get("Cache-Control") != "public, max-age=300" || rec.Header().Get("Vary") != "Cookie, Accept-Language" {
			t.Errorf("%s: signed-out cache headers %q, Vary %q", tc.path, rec.Header().Get("Cache-Control"), rec.Header().Get("Vary"))
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
		canonical := tc.path
		if tc.path == "/about" {
			canonical = "/" // About is the signed-out home page's content
		}
		if !tc.noIndex && !strings.Contains(body, `<link rel="canonical" href="https://app.example`+canonical+`"`) {
			t.Errorf("%s: missing canonical link to %s", tc.path, canonical)
		}
		if !tc.noIndex && (!strings.Contains(body, `<meta property="og:image" content="https://app.example/static/og-image.jpg?v=test"`) || !strings.Contains(body, `content="summary_large_image"`)) {
			t.Errorf("%s: no large link-preview image", tc.path)
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
	for _, path := range []string{"/signin", "/signup", "/demo", "/forgot", "/reset", "/verify-pending/resend", "/verify-pending/email", "/settings", "/settings/avatar", "/settings/avatar/remove"} {
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

// A signed-out visitor gets the language their browser asks for — and the next visitor, asking
// for another, isn't served the first one's cached copy.
func TestPagesFollowAcceptLanguage(t *testing.T) {
	s := newPagesTestServer(t)
	get := func(path, acceptLanguage string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Language", acceptLanguage)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	for _, path := range []string{"/help", "/signin", "/nowhere"} {
		if ru := get(path, "ru-RU,ru;q=0.9,en;q=0.8"); !strings.Contains(ru, `<html lang="ru">`) || !strings.Contains(ru, ">Войти</a>") {
			t.Errorf("%s: Russian browser didn't get a Russian page", path)
		}
		if en := get(path, "en-US"); !strings.Contains(en, `<html lang="en">`) || !strings.Contains(en, ">Sign in</a>") {
			t.Errorf("%s: English browser didn't get an English page", path)
		}
		if other := get(path, "de-DE"); !strings.Contains(other, `<html lang="en">`) {
			t.Errorf("%s: an unsupported language didn't fall back to English", path)
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

func TestAccountErrorLanguages(t *testing.T) {
	err := accountFailure(http.StatusBadRequest, "error.password_too_short", "min", 8)
	var ae *accountError
	if !errors.As(err, &ae) {
		t.Fatal("not an accountError")
	}
	if got := ae.message("en"); got != "Password must be at least 8 characters." {
		t.Errorf("en: %q", got)
	}
	if got := ae.message("ru"); got != "Пароль должен быть не короче 8 символов." {
		t.Errorf("ru: %q", got)
	}
	if err.Error() != ae.message("en") {
		t.Errorf("Error() = %q, want the English message", err.Error())
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

func TestAppShellRoutes(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct {
		path, want string
	}{
		// No session: the app's pages send you to sign in (`/` is the home page instead —
		// TestSignedOutHome).
		{"/profile", "/signin"},
		{"/settings", "/signin"},
		// Links from emails sent before the auth pages existed, forwarded before any session
		// check — they work signed out.
		{"/?reset_token=abc", "/reset?token=abc"},
		{"/?verify_token=a%20b", "/verify?token=a+b"},
		{"/?auth_error=google", "/signin?error=google"},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
			t.Errorf("%s: status %d, location %q; want 303 to %q", tc.path, rec.Code, rec.Header().Get("Location"), tc.want)
		}
	}
}

func TestNotFound(t *testing.T) {
	s := newPagesTestServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Page not found") || !strings.Contains(rec.Body.String(), `class="page-header"`) {
		t.Errorf("/no-such-page: status %d, want the 404 page with the shared header", rec.Code)
	}
	for _, path := range []string{"/v1/no-such-endpoint", "/tiles/v1/nope/1/2/3"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s: status %d; want a plain 404, not a page", path, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/header.css", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/static/header.css: status %d", rec.Code)
	}
}

func TestNormalizeTimezoneStoresCurrentNames(t *testing.T) {
	for in, want := range map[string]string{
		"Asia/Calcutta":    "Asia/Kolkata",
		" Europe/Kiev ":    "Europe/Kyiv",
		"America/New_York": "America/New_York",
		"Asia/Kolkata":     "Asia/Kolkata",
	} {
		if got, ok := normalizeTimezone(in); !ok || got != want {
			t.Errorf("normalizeTimezone(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := normalizeTimezone("Mars/Olympus"); ok {
		t.Errorf("an unknown zone was accepted")
	}
}

func TestSignedOutHome(t *testing.T) {
	s := newPagesTestServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("/: status %d, want the home page", rec.Code)
	}
	for _, want := range []string{
		"<title>HoldMyTrack — Every journey, mapped.</title>",
		"Every place you have ever run, ridden or walked", // About's own content
		`<link rel="canonical" href="https://app.example/"`,
		">Sign in</a>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/: missing %q", want)
		}
	}
	// The structured data is valid JSON, with absolute URLs.
	_, ld, _ := strings.Cut(body, `<script type="application/ld+json">`)
	ld, _, _ = strings.Cut(ld, "</script>")
	var app struct {
		Type  string `json:"@type"`
		URL   string `json:"url"`
		Image string `json:"image"`
	}
	if err := json.Unmarshal([]byte(ld), &app); err != nil {
		t.Fatalf("/: structured data isn't JSON: %v\n%s", err, ld)
	}
	if app.Type != "WebApplication" || app.URL != "https://app.example/" || app.Image != "https://app.example/static/og-image.jpg?v=test" {
		t.Errorf("/: structured data %+v", app)
	}
	if strings.Contains(body, `id="root"`) {
		t.Errorf("/: signed out got the app shell")
	}
	// Sign-up is reachable but not indexed: /signin is the page results should show.
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/signup", nil))
	if !strings.Contains(rec.Body.String(), `name="robots" content="noindex"`) {
		t.Errorf("/signup is indexable")
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/signin", nil))
	if strings.Contains(rec.Body.String(), `content="noindex"`) {
		t.Errorf("/signin isn't indexable")
	}
}
