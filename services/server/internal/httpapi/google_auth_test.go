package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testClientID = "client-123.apps.googleusercontent.com"

func fakeIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".sig"
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"exp":            now.Add(time.Hour).Unix(),
		"sub":            "1234567890",
		"email":          " Someone@Example.com ",
		"email_verified": true,
		"name":           "Some One",
	}
}

func TestParseGoogleIDToken(t *testing.T) {
	now := time.Now()
	c, err := parseGoogleIDToken(fakeIDToken(t, validClaims(now)), testClientID, now)
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if c.Sub != "1234567890" || c.Email != "someone@example.com" || c.Name != "Some One" {
		t.Fatalf("unexpected claims: %+v", c)
	}

	stringVerified := validClaims(now)
	stringVerified["email_verified"] = "true"
	stringVerified["iss"] = "accounts.google.com"
	if _, err := parseGoogleIDToken(fakeIDToken(t, stringVerified), testClientID, now); err != nil {
		t.Fatalf("string email_verified / bare issuer rejected: %v", err)
	}

	for name, mutate := range map[string]func(map[string]any){
		"wrong issuer":     func(c map[string]any) { c["iss"] = "https://evil.example" },
		"wrong audience":   func(c map[string]any) { c["aud"] = "someone-else" },
		"expired":          func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() },
		"no subject":       func(c map[string]any) { delete(c, "sub") },
		"unverified email": func(c map[string]any) { c["email_verified"] = false },
		"missing verified": func(c map[string]any) { delete(c, "email_verified") },
		"invalid email":    func(c map[string]any) { c["email"] = "not-an-email" },
	} {
		claims := validClaims(now)
		mutate(claims)
		if _, err := parseGoogleIDToken(fakeIDToken(t, claims), testClientID, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseGoogleIDToken("not-a-jwt", testClientID, now); err == nil {
		t.Error("malformed token accepted")
	}
}

func testGoogleServer(tokenURL string) *Server {
	g := newGoogleOAuth(GoogleOAuthConfig{
		ClientID: testClientID, ClientSecret: "secret", RedirectURL: "https://app.example/v1/auth/google/callback",
	})
	if tokenURL != "" {
		g.tokenURL = tokenURL
	}
	return &Server{log: slog.New(slog.DiscardHandler), appBaseURL: "https://app.example", google: g}
}

func TestGoogleStartRedirectsWithPKCE(t *testing.T) {
	s := testGoogleServer("")
	rec := httptest.NewRecorder()
	s.handleGoogleStart(rec, httptest.NewRequest("GET", "/v1/auth/google/start?tz=Europe/Berlin", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), googleAuthURL) {
		t.Fatalf("bad redirect %q", rec.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("client_id") != testClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("bad auth params: %v", q)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oauthCookieName || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("bad oauth cookie: %+v", cookies)
	}
	parts := strings.Split(cookies[0].Value, ".")
	if len(parts) != 3 || parts[0] != q.Get("state") || parts[2] != "Europe/Berlin" {
		t.Fatalf("cookie %q does not carry state/verifier/tz", cookies[0].Value)
	}
}

func TestGoogleEndpointsDisabledWithoutClientID(t *testing.T) {
	s := &Server{log: slog.New(slog.DiscardHandler), google: newGoogleOAuth(GoogleOAuthConfig{})}
	for _, h := range []http.HandlerFunc{s.handleGoogleStart, s.handleGoogleCallback} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want 404", rec.Code)
		}
	}
}

func TestGoogleCallbackRejectsStateMismatch(t *testing.T) {
	s := testGoogleServer("")
	req := httptest.NewRequest("GET", "/v1/auth/google/callback?state=forged&code=x", nil)
	req.AddCookie(&http.Cookie{Name: oauthCookieName, Value: "real.verifier.UTC"})
	rec := httptest.NewRecorder()
	s.handleGoogleCallback(rec, req)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || loc != "https://app.example/signin?error=google" {
		t.Fatalf("got %d %q", rec.Code, loc)
	}
}

func TestExchangeGoogleCode(t *testing.T) {
	now := time.Now()
	var got url.Values
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		json.NewEncoder(w).Encode(map[string]string{"id_token": fakeIDToken(t, validClaims(now))})
	}))
	defer token.Close()

	c, err := testGoogleServer(token.URL).exchangeGoogleCode(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sub != "1234567890" {
		t.Fatalf("unexpected claims %+v", c)
	}
	if got.Get("code") != "the-code" || got.Get("code_verifier") != "the-verifier" || got.Get("grant_type") != "authorization_code" {
		t.Fatalf("unexpected token request %v", got)
	}
}
