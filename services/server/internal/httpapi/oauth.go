package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

// The provider-independent half of the server-side sign-in flows — Google (google_auth.go,
// docs/SPEC.md FR-1.9) and Facebook (facebook_auth.go, FR-1.10). Each provider's start handler
// calls beginOAuth, its callback calls readOAuthCallback, resolves the account its own way,
// and ends in finishOAuthSignIn.

const (
	// oauthCookieName carries state + PKCE verifier + the browser's timezone across the
	// round trip through the provider. Each provider scopes it to its own /v1/auth/<provider>
	// path, so no other request ever sends it and two flows never read each other's.
	oauthCookieName = "holdmytrack_oauth"
	// oauthCookieTTL bounds how long someone can sit on the provider's consent screen before
	// the callback stops accepting the round trip.
	oauthCookieTTL = 10 * time.Minute
)

// beginOAuth starts a round trip: a fresh state, a PKCE verifier when withPKCE (empty
// otherwise), and the browser's timezone from ?tz= (anything invalid is dropped here and falls
// back to UTC at account creation), all stored in the oauth cookie under cookiePath. ok false
// means the response has already been written.
func (s *Server) beginOAuth(w http.ResponseWriter, r *http.Request, cookiePath string, withPKCE bool) (state, verifier string, ok bool) {
	state, err := randomToken()
	if err != nil {
		s.log.Error("oauth state generation failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return "", "", false
	}
	if withPKCE {
		if verifier, err = randomToken(); err != nil {
			s.log.Error("pkce verifier generation failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return "", "", false
		}
	}
	tz, _ := normalizeTimezone(r.URL.Query().Get("tz"))
	s.setOAuthCookie(w, cookiePath, strings.Join([]string{state, verifier, tz}, "."), oauthCookieTTL)
	return state, verifier, true
}

// readOAuthCallback checks a callback against the cookie beginOAuth set — clearing the cookie
// first, whatever the outcome, so a round trip is single-use — and returns the authorization
// code plus what the cookie carried. Any error means the round trip must not continue: the
// provider reported one (the user cancelled, most often), the cookie is missing, or state
// doesn't match.
func (s *Server) readOAuthCallback(w http.ResponseWriter, r *http.Request, cookiePath string) (code, verifier, tz string, err error) {
	cookie, cookieErr := r.Cookie(oauthCookieName)
	s.setOAuthCookie(w, cookiePath, "", -1)

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return "", "", "", errors.New("provider returned an error: " + e)
	}
	if cookieErr != nil {
		return "", "", "", errors.New("missing oauth cookie")
	}
	parts := strings.SplitN(cookie.Value, ".", 3)
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		return "", "", "", errors.New("state mismatch")
	}
	if q.Get("code") == "" {
		return "", "", "", errors.New("no code in callback")
	}
	return q.Get("code"), parts[1], parts[2], nil
}

// finishOAuthSignIn starts the same session every other sign-in path does and sends the
// browser to the app, which routes an unverified or not-yet-onboarded account onward itself.
func (s *Server) finishOAuthSignIn(w http.ResponseWriter, r *http.Request, userID string) error {
	if _, err := s.startSession(w, r.Context(), userID, sessionTTL); err != nil {
		return err
	}
	http.Redirect(w, r, s.appBaseURL+"/", http.StatusFound)
	return nil
}

// setOAuthCookie sets the round-trip cookie, or deletes it for a negative maxAge.
func (s *Server) setOAuthCookie(w http.ResponseWriter, path, value string, maxAge time.Duration) {
	seconds := int(maxAge / time.Second)
	if maxAge < 0 {
		// Max-Age<=0 deletes; http.Cookie spells that -1 (its 0 means "no Max-Age", a
		// session cookie). Dividing -1ns by a second would have rounded to exactly that 0.
		seconds = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookieName,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.appBaseURL, "https://"), // same derivation as startSession
		// Lax, not Strict: the callback is a top-level cross-site GET navigation from the
		// provider's own site, which Lax still sends the cookie on and Strict would not.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   seconds,
	})
}

// newAccountDisplayName is a provider profile's name as a new account's display_name: trimmed,
// nil when empty, and cut to users.display_name's 255 characters — a longer profile name
// shouldn't fail the whole sign-in over a cosmetic field.
func newAccountDisplayName(name string) *string {
	r := []rune(strings.TrimSpace(name))
	if len(r) == 0 {
		return nil
	}
	v := string(r[:min(len(r), 255)])
	return &v
}

// randomToken returns 32 random bytes, base64url-encoded — used for both the OAuth state and
// the PKCE verifier (43 characters, inside RFC 7636's 43–128 range).
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
