package httpapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCerts stands in for Google's certs endpoint, serving whichever keys are in it now and
// counting fetches.
type fakeCerts struct {
	keys    map[string]*rsa.PublicKey
	fetches atomic.Int32
}

func (c *fakeCerts) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.fetches.Add(1)
		var set struct {
			Keys []map[string]string `json:"keys"`
		}
		for kid, key := range c.keys {
			set.Keys = append(set.Keys, map[string]string{
				"kid": kid, "kty": "RSA", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// signedIDToken builds an id_token with the given header and claims, RS256-signed with key.
func signedIDToken(t *testing.T, key *rsa.PrivateKey, header map[string]string, claims map[string]any) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signingInput := enc(h) + "." + enc(c)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + enc(sig)
}

func testGoogleKeyServer(certsURL string) *Server {
	s := testGoogleServer("")
	s.google.keys.url = certsURL
	return s
}

func TestVerifyGoogleIDToken(t *testing.T) {
	now := time.Now()
	key, other := newRSAKey(t), newRSAKey(t)
	certs := &fakeCerts{keys: map[string]*rsa.PublicKey{"k1": &key.PublicKey}}
	srv := certs.serve(t)
	rs256 := map[string]string{"alg": "RS256", "kid": "k1"}

	t.Run("valid", func(t *testing.T) {
		c, err := testGoogleKeyServer(srv.URL).verifyGoogleIDToken(context.Background(), signedIDToken(t, key, rs256, validClaims(now)), now)
		if err != nil || c.Sub != "1234567890" || c.Email != "someone@example.com" {
			t.Fatalf("got %+v, %v", c, err)
		}
	})

	for name, token := range map[string]string{
		"signed with another key": signedIDToken(t, other, rs256, validClaims(now)),
		"alg none": func() string {
			segs := strings.Split(fakeIDToken(t, validClaims(now)), ".")
			return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"k1"}`)) + "." + segs[1] + "."
		}(),
		"HS256": signedIDToken(t, key, map[string]string{"alg": "HS256", "kid": "k1"}, validClaims(now)),
		"payload swapped after signing": func() string {
			segs := strings.Split(signedIDToken(t, key, rs256, validClaims(now)), ".")
			forged := validClaims(now)
			forged["sub"] = "someone-else"
			p, _ := json.Marshal(forged)
			return segs[0] + "." + base64.RawURLEncoding.EncodeToString(p) + "." + segs[2]
		}(),
		"signature fine, audience wrong": signedIDToken(t, key, rs256, func() map[string]any {
			c := validClaims(now)
			c["aud"] = "someone-elses-client"
			return c
		}()),
		"not a JWT": "garbage",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := testGoogleKeyServer(srv.URL).verifyGoogleIDToken(context.Background(), token, now); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestGoogleKeySetRefetchesOnNewKid(t *testing.T) {
	now := time.Now()
	oldKey, newKey := newRSAKey(t), newRSAKey(t)
	certs := &fakeCerts{keys: map[string]*rsa.PublicKey{"old": &oldKey.PublicKey}}
	s := testGoogleKeyServer(certs.serve(t).URL)
	ctx := context.Background()

	if _, err := s.verifyGoogleIDToken(ctx, signedIDToken(t, oldKey, map[string]string{"alg": "RS256", "kid": "old"}, validClaims(now)), now); err != nil {
		t.Fatal(err)
	}
	// Google rotates: the new key is published, and a token signed with it arrives while the
	// cache still holds only the old set.
	certs.keys["new"] = &newKey.PublicKey
	token := signedIDToken(t, newKey, map[string]string{"alg": "RS256", "kid": "new"}, validClaims(now))
	if _, err := s.verifyGoogleIDToken(ctx, token, now); err == nil {
		t.Fatal("unknown kid accepted within the refetch gap")
	}
	if got := certs.fetches.Load(); got != 1 {
		t.Fatalf("%d fetches within the refetch gap, want 1", got)
	}
	later := now.Add(googleKeysRefetchGap + time.Second)
	if _, err := s.verifyGoogleIDToken(ctx, token, later); err != nil {
		t.Fatalf("new kid after the gap: %v", err)
	}
	if got := certs.fetches.Load(); got != 2 {
		t.Fatalf("%d fetches, want 2", got)
	}
}

func TestCacheMaxAge(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"public, max-age=19204, must-revalidate, no-transform": 19204 * time.Second,
		"no-cache":   time.Hour,
		"max-age=0":  time.Hour,
		"max-age=xx": time.Hour,
		"":           time.Hour,
	} {
		if got := cacheMaxAge(header, time.Hour); got != want {
			t.Errorf("%q: got %v, want %v", header, got, want)
		}
	}
}

func TestHandleGoogleToken(t *testing.T) {
	disabled := &Server{google: newGoogleOAuth(GoogleOAuthConfig{})}
	rec := httptest.NewRecorder()
	disabled.handleGoogleToken(rec, httptest.NewRequest("POST", "/v1/auth/google/token", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled: status %d, want 404", rec.Code)
	}

	// A token that fails verification never reaches account resolution (no database here).
	certs := &fakeCerts{keys: map[string]*rsa.PublicKey{"k1": &newRSAKey(t).PublicKey}}
	s := testGoogleKeyServer(certs.serve(t).URL)
	rec = httptest.NewRecorder()
	s.handleGoogleToken(rec, httptest.NewRequest("POST", "/v1/auth/google/token", strings.NewReader(`{"id_token":"a.b.c","tz":"Europe/Berlin"}`)))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Google") {
		t.Fatalf("bad token: %d %q", rec.Code, rec.Body.String())
	}
}

func TestAuthProviders(t *testing.T) {
	for name, tc := range map[string]struct {
		s    *Server
		want string
	}{
		"both on":  {&Server{google: testGoogleServer("").google, facebook: testFacebookServer("").facebook}, `{"google":true,"facebook":true,"google_client_id":"` + testClientID + `"}`},
		"both off": {&Server{google: newGoogleOAuth(GoogleOAuthConfig{}), facebook: newFacebookOAuth(FacebookOAuthConfig{})}, `{"google":false,"facebook":false}`},
	} {
		rec := httptest.NewRecorder()
		tc.s.handleAuthProviders(rec, httptest.NewRequest("GET", "/v1/auth/providers", nil))
		if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
			t.Errorf("%s: got %s, want %s", name, got, tc.want)
		}
	}
}
