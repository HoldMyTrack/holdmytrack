package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testChallenge is a well-formed S256 challenge, as the Android app sends one.
func testChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestBeginOAuthAppChallenge(t *testing.T) {
	challenge := testChallenge("the-verifier")

	rec := httptest.NewRecorder()
	testFacebookServer("").handleFacebookStart(rec, httptest.NewRequest("GET", "/v1/auth/facebook/start?tz=UTC&app_challenge="+challenge, nil))
	cookies := rec.Result().Cookies()
	if rec.Code != http.StatusFound || len(cookies) != 1 || !strings.HasSuffix(cookies[0].Value, "..UTC."+challenge) {
		t.Fatalf("got %d, cookies %+v", rec.Code, cookies)
	}

	for _, bad := range []string{"short", strings.Repeat("a", 42) + "!", strings.Repeat("a", 44)} {
		rec := httptest.NewRecorder()
		testFacebookServer("").handleFacebookStart(rec, httptest.NewRequest("GET", "/v1/auth/facebook/start?app_challenge="+bad, nil))
		if rec.Code != http.StatusBadRequest || len(rec.Result().Cookies()) != 0 {
			t.Errorf("%q: got %d, want 400 and no cookie", bad, rec.Code)
		}
	}
}

// An app's round trip that fails goes back to the app, with the same error codes the sign-in
// page gets, rather than leaving the browser tab on the web's sign-in page.
func TestNativeCallbackFailuresReturnToApp(t *testing.T) {
	graph := &fakeGraph{me: map[string]string{"id": "42", "name": "Some One"}} // no email
	srv := graph.serve(t)
	appCookie := "s..UTC." + testChallenge("the-verifier")
	for name, tc := range map[string]struct {
		query, cookie, want string
	}{
		"cancelled":     {"?error=access_denied&state=s", appCookie, "holdmytrack://oauth?error=facebook"},
		"state forged":  {"?state=forged&code=x", appCookie, "holdmytrack://oauth?error=facebook"},
		"email missing": {"?state=s&code=x", appCookie, "holdmytrack://oauth?error=facebook_no_email"},
		// Without the cookie there's no telling an app started it; the web page is all that's left.
		"no cookie": {"?state=s&code=x", "", "https://app.example/signin?error=facebook"},
		// A cookie from before app handoffs existed still reads as a browser's round trip.
		"three-part cookie": {"?state=s&code=x", "s..UTC", "https://app.example/signin?error=facebook_no_email"},
	} {
		req := httptest.NewRequest("GET", "/v1/auth/facebook/callback"+tc.query, nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: oauthCookieName, Value: tc.cookie})
		}
		rec := httptest.NewRecorder()
		testFacebookServer(srv.URL).handleFacebookCallback(rec, req)
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || loc != tc.want {
			t.Errorf("%s: got %d %q, want %q", name, rec.Code, loc, tc.want)
		}
	}
}

// Malformed requests are refused before the database is touched (there's none here).
func TestAuthHandoffRejectsMalformed(t *testing.T) {
	verifier := strings.Repeat("v", 43)
	for name, body := range map[string]string{
		"not a uuid":     `{"code":"x","verifier":"` + verifier + `"}`,
		"short verifier": `{"code":"8f14e45f-ceea-467a-9575-6f8d4a3c1b2e","verifier":"short"}`,
		"no verifier":    `{"code":"8f14e45f-ceea-467a-9575-6f8d4a3c1b2e"}`,
	} {
		rec := httptest.NewRecorder()
		testFacebookServer("").handleAuthHandoff(rec, httptest.NewRequest("POST", "/v1/auth/handoff", strings.NewReader(body)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	testFacebookServer("").handleAuthHandoff(rec, httptest.NewRequest("POST", "/v1/auth/handoff", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: status %d, want 400", rec.Code)
	}
}

// Behind the proxy, each visitor is its own X-Forwarded-For address — the last one, the one
// Caddy itself appended — and only a private or loopback peer is believed about it.
func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		remote, xff, want string
	}{
		{"172.18.0.5:41234", "203.0.113.7", "203.0.113.7"},
		{"172.18.0.5:41234", "1.2.3.4, 203.0.113.7", "203.0.113.7"},
		{"127.0.0.1:5000", "2001:db8::1", "2001:db8::1"},
		{"172.18.0.5:41234", "", "172.18.0.5"},
		{"172.18.0.5:41234", "not-an-ip", "172.18.0.5"},
		{"198.51.100.9:443", "203.0.113.7", "198.51.100.9"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/demo", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("RemoteAddr %s, X-Forwarded-For %q: %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

func TestNormalizeEmailTakesOnlyABareAddress(t *testing.T) {
	for in, want := range map[string]string{
		" Someone@Example.com ":          "someone@example.com",
		"bob <victim@example.com>":       "",
		"<victim@example.com>":           "",
		"victim@example.com (x)":         "",
		"not-an-email":                   "",
		"first.last+tag@sub.example.org": "first.last+tag@sub.example.org",
	} {
		got, err := normalizeEmail(in)
		if (want == "") != (err != nil) || got != want {
			t.Errorf("normalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// A password past bcrypt's 72 bytes is a validation error, not the 500 GenerateFromPassword's
// refusal used to become.
func TestPasswordLength(t *testing.T) {
	for _, tc := range []struct {
		password string
		ok       bool
	}{
		{"short", false},
		{"eight888", true},
		{strings.Repeat("a", maxPasswordBytes), true},
		{strings.Repeat("a", maxPasswordBytes+1), false},
		{strings.Repeat("я", maxPasswordBytes/2+1), false},
	} {
		if err := checkPasswordLength(tc.password); (err == nil) != tc.ok {
			t.Errorf("%d bytes: err %v, want ok=%v", len(tc.password), err, tc.ok)
		}
	}
}
