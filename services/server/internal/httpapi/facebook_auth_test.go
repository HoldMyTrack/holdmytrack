package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testAppID = "1234567890123456"

func testFacebookServer(graphURL string) *Server {
	f := newFacebookOAuth(FacebookOAuthConfig{
		AppID: testAppID, AppSecret: "secret", RedirectURL: "https://app.example/v1/auth/facebook/callback",
	})
	if graphURL != "" {
		f.graphURL = graphURL
	}
	return &Server{log: slog.New(slog.DiscardHandler), appBaseURL: "https://app.example", facebook: f}
}

// fakeGraph stands in for graph.facebook.com: the token endpoint and /me, recording what each
// was sent. me is /me's response body; a nil me makes /me fail.
type fakeGraph struct {
	tokenQuery url.Values
	meQuery    url.Values
	meAuth     string
	me         map[string]string
}

func (g *fakeGraph) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/access_token":
			g.tokenQuery = r.URL.Query()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "the-token", "token_type": "bearer", "expires_in": 5183944})
		case "/me":
			g.meQuery, g.meAuth = r.URL.Query(), r.Header.Get("Authorization")
			if g.me == nil {
				http.Error(w, `{"error":{"message":"Invalid OAuth access token"}}`, http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(g.me)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFacebookStartRedirectsToDialog(t *testing.T) {
	s := testFacebookServer("")
	rec := httptest.NewRecorder()
	s.handleFacebookStart(rec, httptest.NewRequest("GET", "/v1/auth/facebook/start?tz=Europe/Berlin", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), facebookDialogURL+"?") {
		t.Fatalf("bad redirect %q", rec.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("client_id") != testAppID || q.Get("response_type") != "code" || q.Get("scope") != "email,public_profile" ||
		q.Get("redirect_uri") != "https://app.example/v1/auth/facebook/callback" || q.Get("state") == "" {
		t.Fatalf("bad dialog params: %v", q)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oauthCookieName || cookies[0].Path != "/v1/auth/facebook" || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("bad oauth cookie: %+v", cookies)
	}
	if want := q.Get("state") + "..Europe/Berlin."; cookies[0].Value != want {
		t.Fatalf("cookie %q, want %q (state, no verifier, tz, no app challenge)", cookies[0].Value, want)
	}
}

func TestFacebookEndpointsDisabledWithoutAppID(t *testing.T) {
	s := &Server{log: slog.New(slog.DiscardHandler), facebook: newFacebookOAuth(FacebookOAuthConfig{})}
	for _, h := range []http.HandlerFunc{s.handleFacebookStart, s.handleFacebookCallback} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want 404", rec.Code)
		}
	}
}

func TestFacebookCallbackFailures(t *testing.T) {
	graph := &fakeGraph{me: map[string]string{"id": "42", "name": "Some One"}} // no email
	srv := graph.serve(t)
	for name, tc := range map[string]struct {
		query, cookie, want string
	}{
		"cancelled":     {"?error=access_denied&state=s", "s..UTC", "facebook"},
		"no cookie":     {"?state=s&code=x", "", "facebook"},
		"state forged":  {"?state=forged&code=x", "s..UTC", "facebook"},
		"no code":       {"?state=s", "s..UTC", "facebook"},
		"email missing": {"?state=s&code=x", "s..UTC", "facebook_no_email"},
	} {
		req := httptest.NewRequest("GET", "/v1/auth/facebook/callback"+tc.query, nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: oauthCookieName, Value: tc.cookie})
		}
		rec := httptest.NewRecorder()
		testFacebookServer(srv.URL).handleFacebookCallback(rec, req)
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || loc != "https://app.example/signin?error="+tc.want {
			t.Errorf("%s: got %d %q", name, rec.Code, loc)
		}
		// The round trip is single-use whatever happened.
		if c := rec.Result().Cookies(); len(c) != 1 || c[0].Name != oauthCookieName || c[0].MaxAge >= 0 {
			t.Errorf("%s: oauth cookie not cleared: %+v", name, c)
		}
	}
}

func TestFetchFacebookProfile(t *testing.T) {
	graph := &fakeGraph{me: map[string]string{"id": "42", "name": "Some One", "email": " Someone@Example.com "}}
	s := testFacebookServer(graph.serve(t).URL)

	p, err := s.fetchFacebookProfile(context.Background(), "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "42" || p.Name != "Some One" || p.Email != "someone@example.com" {
		t.Fatalf("unexpected profile %+v", p)
	}
	tq := graph.tokenQuery
	if tq.Get("code") != "the-code" || tq.Get("client_id") != testAppID || tq.Get("client_secret") != "secret" ||
		tq.Get("redirect_uri") != "https://app.example/v1/auth/facebook/callback" {
		t.Fatalf("unexpected token request %v", tq)
	}
	// The token travels in the header, never the URL; appsecret_proof in the URL is keyed
	// with the secret, so it's no use to anyone who reads it.
	if graph.meAuth != "Bearer the-token" || graph.meQuery.Get("access_token") != "" {
		t.Fatalf("token sent as %q / %v", graph.meAuth, graph.meQuery)
	}
	if graph.meQuery.Get("fields") != "id,name,email" || graph.meQuery.Get("appsecret_proof") != facebookAppSecretProof("the-token", "secret") {
		t.Fatalf("unexpected /me query %v", graph.meQuery)
	}

	graph.me = map[string]string{"id": "42", "email": "not-an-email"}
	if _, err := s.fetchFacebookProfile(context.Background(), "c"); !errors.Is(err, errFacebookNoEmail) {
		t.Errorf("invalid email: %v", err)
	}
	graph.me = nil
	if _, err := s.fetchFacebookProfile(context.Background(), "c"); err == nil || errors.Is(err, errFacebookNoEmail) {
		t.Errorf("failing /me: %v", err)
	}
}

func TestFacebookAppSecretProof(t *testing.T) {
	// HMAC-SHA256("token", key "secret"), computed independently with openssl:
	// printf token | openssl dgst -sha256 -hmac secret
	if got, want := facebookAppSecretProof("token", "secret"), "e941110e3d2bfe82621f0e3e1434730d7305d106c5f68c87165d0b27a4611a4a"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
