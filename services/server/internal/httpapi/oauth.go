package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// The provider-independent half of the server-side sign-in flows — Google (google_auth.go,
// docs/SPEC.md FR-1.9) and Facebook (facebook_auth.go, FR-1.10). Each provider's start handler
// calls beginOAuth, its callback calls readOAuthCallback, resolves the account its own way,
// and ends in finishOAuth — or failOAuth.
//
// A native app can run the same round trip in a browser tab (docs/adr/0016-native-sign-in.md):
// it starts it with ?app_challenge=, the S256 challenge of a verifier only the app holds, and
// instead of a session cookie in that tab the callback hands the app a one-time code at
// appRedirectURI, which only the verifier redeems (handleAuthHandoff).

const (
	// oauthCookieName carries state + PKCE verifier + the browser's timezone (+ an app's
	// challenge) across the round trip through the provider. Each provider scopes it to its
	// own /v1/auth/<provider> path, so no other request ever sends it and two flows never read
	// each other's.
	oauthCookieName = "holdmytrack_oauth"
	// oauthCookieTTL bounds how long someone can sit on the provider's consent screen before
	// the callback stops accepting the round trip.
	oauthCookieTTL = 10 * time.Minute

	// appRedirectURI is where a native round trip ends: the Android app's intent filter
	// (scheme holdmytrack, host oauth), with ?code= or ?error= appended.
	appRedirectURI = "holdmytrack://oauth"
	// handoffTTL bounds the gap between the callback and the app redeeming its code — the tab
	// closing and one request, so seconds in practice.
	handoffTTL = 2 * time.Minute
)

// oauthRoundTrip is what a callback carries: the provider's code plus what beginOAuth stored
// in the cookie. appChallenge is set when a native app started the round trip.
type oauthRoundTrip struct {
	code, verifier, tz, appChallenge string
}

// beginOAuth starts a round trip: a fresh state, a PKCE verifier when withPKCE (empty
// otherwise), the browser's timezone from ?tz= (anything invalid is dropped here and falls
// back to UTC at account creation), and an app's ?app_challenge= when there is one, all stored
// in the oauth cookie under cookiePath. ok false means the response has already been written.
func (s *Server) beginOAuth(w http.ResponseWriter, r *http.Request, cookiePath string, withPKCE bool) (state, verifier string, ok bool) {
	appChallenge := r.URL.Query().Get("app_challenge")
	if appChallenge != "" && !validPKCEValue(appChallenge) {
		http.Error(w, "invalid app_challenge", http.StatusBadRequest)
		return "", "", false
	}
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
	s.setOAuthCookie(w, cookiePath, strings.Join([]string{state, verifier, tz, appChallenge}, "."), oauthCookieTTL)
	return state, verifier, true
}

// readOAuthCallback checks a callback against the cookie beginOAuth set — clearing the cookie
// first, whatever the outcome, so a round trip is single-use — and returns the authorization
// code plus what the cookie carried. Any error means the round trip must not continue: the
// provider reported one (the user cancelled, most often), the cookie is missing, or state
// doesn't match. The round trip's appChallenge is filled in even then, whenever the cookie was
// readable, so failOAuth can still send a native round trip back to its app.
func (s *Server) readOAuthCallback(w http.ResponseWriter, r *http.Request, cookiePath string) (oauthRoundTrip, error) {
	cookie, cookieErr := r.Cookie(oauthCookieName)
	s.setOAuthCookie(w, cookiePath, "", -1)

	var parts []string
	var rt oauthRoundTrip
	if cookieErr == nil {
		// Three parts from a cookie set before app handoffs existed, four since.
		parts = strings.SplitN(cookie.Value, ".", 4)
		if len(parts) == 4 {
			rt.appChallenge = parts[3]
		}
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return rt, errors.New("provider returned an error: " + e)
	}
	if cookieErr != nil {
		return rt, errors.New("missing oauth cookie")
	}
	if len(parts) < 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		return rt, errors.New("state mismatch")
	}
	if q.Get("code") == "" {
		return rt, errors.New("no code in callback")
	}
	rt.code, rt.verifier, rt.tz = q.Get("code"), parts[1], parts[2]
	return rt, nil
}

// finishOAuth ends a successful round trip. A browser gets the same session every other
// sign-in path starts and is sent to the app, which routes an unverified or not-yet-onboarded
// account onward itself. A native app gets a one-time code for the account instead, at
// appRedirectURI.
func (s *Server) finishOAuth(w http.ResponseWriter, r *http.Request, rt oauthRoundTrip, userID string) error {
	if rt.appChallenge != "" {
		code, err := s.createHandoff(r.Context(), userID, rt.appChallenge)
		if err != nil {
			return err
		}
		http.Redirect(w, r, appRedirectURI+"?"+url.Values{"code": {code}}.Encode(), http.StatusFound)
		return nil
	}
	if _, err := s.startSession(w, r.Context(), userID, sessionTTL); err != nil {
		return err
	}
	http.Redirect(w, r, s.appBaseURL+"/", http.StatusFound)
	return nil
}

// failOAuth ends a failed round trip with an error code — on the sign-in page for a browser,
// back in the app for a native round trip, which shows its own wording for the same codes.
func (s *Server) failOAuth(w http.ResponseWriter, r *http.Request, rt oauthRoundTrip, code string) {
	if rt.appChallenge != "" {
		http.Redirect(w, r, appRedirectURI+"?"+url.Values{"error": {code}}.Encode(), http.StatusFound)
		return
	}
	http.Redirect(w, r, s.appBaseURL+"/signin?error="+code, http.StatusFound)
}

// createHandoff stores a one-time code for userID, redeemable only with the verifier behind
// challenge, and returns it. Expired codes nobody redeemed are purged here, so the table never
// needs a sweeper of its own.
func (s *Server) createHandoff(ctx context.Context, userID, challenge string) (string, error) {
	if _, err := s.pool.Exec(ctx, `DELETE FROM auth_handoffs WHERE expires_at < NOW()`); err != nil {
		return "", err
	}
	var code string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO auth_handoffs (user_id, challenge, expires_at) VALUES ($1, $2, $3) RETURNING id
	`, userID, challenge, time.Now().Add(handoffTTL)).Scan(&code)
	return code, err
}

// handleAuthHandoff serves `POST /v1/auth/handoff` `{code, verifier}` — where a native app
// redeems the code a browser-tab round trip ended with (finishOAuth) for the same session JSON
// a password sign-in returns. Single-use: the row is deleted by the very query that reads it.
// The verifier is what makes a code useless to anyone else: another app that registered the
// same URL scheme could receive the redirect, but not the verifier, which never left the app
// that started the round trip.
func (s *Server) handleAuthHandoff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	invalid := func(msg string) {
		s.log.Warn("sign-in handoff refused: " + msg)
		http.Error(w, i18n.Get(requestLang(r)).T("error.handoff_invalid"), http.StatusUnauthorized)
	}
	if !uuidPattern.MatchString(req.Code) || !validPKCEValue(req.Verifier) {
		invalid("malformed request")
		return
	}
	var userID, challenge string
	var expiresAt time.Time
	err := s.pool.QueryRow(r.Context(), `
		DELETE FROM auth_handoffs WHERE id = $1 RETURNING user_id, challenge, expires_at
	`, req.Code).Scan(&userID, &challenge, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		invalid("unknown or already used code")
		return
	}
	if err != nil {
		s.log.Error("handoff lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if time.Now().After(expiresAt) {
		invalid("expired code")
		return
	}
	sum := sha256.Sum256([]byte(req.Verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
		invalid("verifier does not match")
		return
	}
	s.writeSession(w, r, userID, sessionTTL, http.StatusOK)
}

// validPKCEValue reports whether v is a 43-character base64url string — both a verifier made
// the way randomToken makes one and its S256 challenge have that shape, and nothing else is
// accepted into the cookie or the handoff table.
func validPKCEValue(v string) bool {
	if len(v) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil
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
