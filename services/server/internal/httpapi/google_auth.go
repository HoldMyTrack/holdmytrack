package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sign in with Google (docs/SPEC.md FR-1.9, docs/adr/0009-google-sign-in-server-side-code-flow.md)
// — the OAuth 2.0 authorization-code flow with PKCE, run entirely server-side: the browser
// navigates to handleGoogleStart, Google redirects back to handleGoogleCallback, and the
// callback ends in the same startSession every other sign-in path uses. No Google JS SDK on the
// client and no OAuth/JWT library here — the id_token arrives straight from Google's token
// endpoint over TLS, which OpenID Connect Core §3.1.3.7 accepts in place of verifying its
// signature, so decoding and checking its claims is all that's left to do.

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"

	// googleCookiePath scopes oauth.go's round-trip cookie to this flow alone.
	googleCookiePath = apiPrefix + "/auth/google"
)

// GoogleOAuthConfig is what cmd/holdmytrack hands New — config.Config's three Google* values.
// An empty ClientID means the feature is off.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// googleOAuth is GoogleOAuthConfig plus the two things a test needs to swap out: the token
// endpoint and the HTTP client that reaches it.
type googleOAuth struct {
	GoogleOAuthConfig
	tokenURL string
	client   *http.Client
}

func newGoogleOAuth(cfg GoogleOAuthConfig) googleOAuth {
	return googleOAuth{GoogleOAuthConfig: cfg, tokenURL: googleTokenURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (g googleOAuth) enabled() bool { return g.ClientID != "" }

// handleAuthProviders serves `GET /v1/auth/providers` — which optional sign-in methods this
// deployment has configured, so a client decides whether to render the Google and Facebook
// buttons from the server's own config rather than a build-time flag that could disagree with it.
func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"google": s.google.enabled(), "facebook": s.facebook.enabled()})
}

// handleGoogleStart serves `GET /v1/auth/google/start?tz=<IANA name>` — a full-page navigation,
// not a fetch(). tz is the browser's own timezone, carried through the round trip so a brand
// new account gets it the same way handleSignup's caller sends it; anything invalid is dropped
// here and falls back to UTC at account creation.
func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	if !s.google.enabled() {
		http.NotFound(w, r)
		return
	}
	state, verifier, ok := s.beginOAuth(w, r, googleCookiePath, true)
	if !ok {
		return
	}

	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {s.google.ClientID},
		"redirect_uri":          {s.google.RedirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		// Always show the account chooser — someone signed into several Google accounts
		// should pick one deliberately, not be silently signed in with whichever was last used.
		"prompt": {"select_account"},
	}
	http.Redirect(w, r, googleAuthURL+"?"+q.Encode(), http.StatusFound)
}

// handleGoogleCallback serves `GET /v1/auth/google/callback` — where Google sends the browser
// back. Every failure, whatever its cause (the user cancelled, a stale or forged state, Google
// unreachable, an unusable id_token), lands on the same `/signin?error=google` redirect: the
// detail goes to the log, not the URL, for handleLogin's same "don't tell a caller more than it
// needs" reasoning.
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.google.enabled() {
		http.NotFound(w, r)
		return
	}
	fail := func(msg string, err error) {
		s.log.Warn("google sign-in failed: "+msg, "err", err)
		http.Redirect(w, r, s.appBaseURL+"/signin?error=google", http.StatusFound)
	}

	code, verifier, tz, err := s.readOAuthCallback(w, r, googleCookiePath)
	if err != nil {
		fail("callback", err)
		return
	}
	ctx := r.Context()
	claims, err := s.exchangeGoogleCode(ctx, code, verifier)
	if err != nil {
		fail("code exchange", err)
		return
	}
	userID, err := s.resolveGoogleUser(ctx, claims, tz)
	if err != nil {
		fail("account resolution", err)
		return
	}
	if err := s.finishOAuthSignIn(w, r, userID); err != nil {
		fail("session start", err)
	}
}

// googleClaims is the subset of Google's id_token payload this flow reads.
type googleClaims struct {
	Iss   string `json:"iss"`
	Aud   string `json:"aud"`
	Exp   int64  `json:"exp"`
	Sub   string `json:"sub"`
	Email string `json:"email"`
	// Google sends a JSON boolean; some older Google endpoints sent the string "true", so
	// accept either rather than failing on a representation change.
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
}

// exchangeGoogleCode trades the authorization code for tokens at Google's token endpoint and
// returns the id_token's validated claims. The access token is discarded — this flow only
// needs to know who the user is, never to call a Google API on their behalf.
func (s *Server) exchangeGoogleCode(ctx context.Context, code, verifier string) (googleClaims, error) {
	if code == "" {
		return googleClaims{}, errors.New("no code in callback")
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {s.google.ClientID},
		"client_secret": {s.google.ClientSecret},
		"redirect_uri":  {s.google.RedirectURL},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.google.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleClaims{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.google.client.Do(req)
	if err != nil {
		return googleClaims{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return googleClaims{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return googleClaims{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return googleClaims{}, fmt.Errorf("token response: %w", err)
	}
	return parseGoogleIDToken(tok.IDToken, s.google.ClientID, time.Now())
}

// parseGoogleIDToken decodes an id_token's payload and applies OpenID Connect Core
// §3.1.3.7's checks that still apply when the token came straight from the token endpoint over
// TLS: issuer, audience, expiry — plus email_verified, since the email is what an existing
// account gets linked by (resolveGoogleUser) and an unverified one proves nothing.
func parseGoogleIDToken(idToken, clientID string, now time.Time) (googleClaims, error) {
	segs := strings.Split(idToken, ".")
	if len(segs) != 3 {
		return googleClaims{}, errors.New("id_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(segs[1])
	if err != nil {
		return googleClaims{}, fmt.Errorf("id_token payload: %w", err)
	}
	var c googleClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return googleClaims{}, fmt.Errorf("id_token payload: %w", err)
	}
	switch {
	case c.Iss != "accounts.google.com" && c.Iss != "https://accounts.google.com":
		return googleClaims{}, fmt.Errorf("unexpected issuer %q", c.Iss)
	case c.Aud != clientID:
		return googleClaims{}, fmt.Errorf("unexpected audience %q", c.Aud)
	case now.Unix() >= c.Exp:
		return googleClaims{}, errors.New("id_token expired")
	case c.Sub == "":
		return googleClaims{}, errors.New("id_token has no subject")
	case c.EmailVerified != true && c.EmailVerified != "true":
		return googleClaims{}, errors.New("google account email is not verified")
	}
	email, err := normalizeEmail(c.Email)
	if err != nil {
		return googleClaims{}, err
	}
	c.Email = email
	return c, nil
}

// resolveGoogleUser maps a Google identity to a HoldMyTrack account, in one transaction:
//
//  1. An account already linked to this Google sub signs in — whatever its email is now.
//  2. Otherwise a real (non-demo) account with the same email gets linked. Google has just
//     vouched that this person controls that mailbox. If the account's own email was never
//     verified, its password is also cleared, its sessions ended and any other identity it
//     links (Facebook, FR-1.10) removed: whoever set that password or linked that identity
//     never proved they own the address, and leaving either would let someone pre-register a
//     victim's email and keep a way in after the victim signs in with Google. An account
//     already linked to a *different* Google sub is refused rather than silently relinked.
//  3. Otherwise a new, already-verified account is created.
func (s *Server) resolveGoogleUser(ctx context.Context, c googleClaims, tz string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider = 'google' AND subject = $1`, c.Sub).Scan(&userID)
	if err == nil {
		return userID, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	var verified, linked bool
	err = tx.QueryRow(ctx, `
		SELECT id, email_verified,
		       EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = users.id AND i.provider = 'google')
		FROM users
		WHERE email = $1 AND demo_expires_at IS NULL
		FOR UPDATE
	`, c.Email).Scan(&userID, &verified, &linked)
	switch {
	case err == nil:
		if linked {
			return "", errors.New("email already linked to a different google account")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, 'google', $2)`, userID, c.Sub); err != nil {
			return "", err
		}
		if !verified {
			for _, q := range []string{
				`UPDATE users SET email_verified = true, password_hash = NULL WHERE id = $1`,
				`DELETE FROM user_identities WHERE user_id = $1 AND provider <> 'google'`,
				`DELETE FROM sessions WHERE user_id = $1`,
				`DELETE FROM password_resets WHERE user_id = $1`,
				`DELETE FROM email_verifications WHERE user_id = $1`,
			} {
				if _, err := tx.Exec(ctx, q, userID); err != nil {
					return "", err
				}
			}
		}
	case errors.Is(err, pgx.ErrNoRows):
		if tz == "" {
			tz = "UTC"
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, email_verified, display_name, timezone)
			VALUES ($1, true, $2, $3) RETURNING id
		`, c.Email, newAccountDisplayName(c.Name), tz).Scan(&userID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, 'google', $2)`, userID, c.Sub); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	return userID, tx.Commit(ctx)
}
