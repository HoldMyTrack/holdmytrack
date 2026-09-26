package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sign in with Facebook (docs/SPEC.md FR-1.10, docs/adr/0015-identities-table-and-facebook-sign-in.md)
// — Facebook Login's manual authorization-code flow, run server-side like Google's
// (google_auth.go): handleFacebookStart redirects to Facebook's login dialog, Facebook redirects
// back to handleFacebookCallback, which trades the code for an access token with the app secret,
// reads the profile from the Graph API once, and ends in the same session every other sign-in
// path starts. No Facebook JS SDK on the client.
//
// Unlike Google, Facebook never says whether the email it returns is verified, so a Facebook
// identity is matched by its Facebook id only — never linked to an existing account by email —
// and a new account it creates verifies its email by mail like any sign-up (FR-1.8).

const (
	// facebookGraphVersion pins the Graph API version the dialog and both endpoints use —
	// Meta retires each version about two years after its release.
	facebookGraphVersion = "v25.0"
	facebookDialogURL    = "https://www.facebook.com/" + facebookGraphVersion + "/dialog/oauth"
	facebookGraphURL     = "https://graph.facebook.com/" + facebookGraphVersion

	// facebookCookiePath scopes oauth.go's round-trip cookie to this flow alone.
	facebookCookiePath = apiPrefix + "/auth/facebook"
)

var (
	// errFacebookNoEmail: the Facebook account has no email (it was registered with a phone
	// number) or the person declined to share it. Every account here needs one.
	errFacebookNoEmail = errors.New("facebook account shared no email")
	// errFacebookEmailInUse: an account here already has this email. Not linked, since
	// Facebook doesn't vouch for the address (see above) — the person signs in the way they
	// already can instead.
	errFacebookEmailInUse = errors.New("email already belongs to an account")
)

// FacebookOAuthConfig is what cmd/holdmytrack hands New — config.Config's three Facebook*
// values. An empty AppID means the feature is off.
type FacebookOAuthConfig struct {
	AppID       string
	AppSecret   string
	RedirectURL string
}

// facebookOAuth is FacebookOAuthConfig plus what a test needs to swap out: the Graph API base
// URL (the token endpoint and /me both live under it) and the HTTP client that reaches it.
type facebookOAuth struct {
	FacebookOAuthConfig
	graphURL string
	client   *http.Client
}

func newFacebookOAuth(cfg FacebookOAuthConfig) facebookOAuth {
	return facebookOAuth{FacebookOAuthConfig: cfg, graphURL: facebookGraphURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (f facebookOAuth) enabled() bool { return f.AppID != "" }

// handleFacebookStart serves `GET /v1/auth/facebook/start?tz=<IANA name>` — a full-page
// navigation, as handleGoogleStart is. No PKCE: Facebook's manual web flow doesn't document
// it, and the code is only redeemable with the app secret, which never leaves this server;
// state still ties the callback to this browser.
func (s *Server) handleFacebookStart(w http.ResponseWriter, r *http.Request) {
	if !s.facebook.enabled() {
		http.NotFound(w, r)
		return
	}
	state, _, ok := s.beginOAuth(w, r, facebookCookiePath, false)
	if !ok {
		return
	}
	q := url.Values{
		"client_id":     {s.facebook.AppID},
		"redirect_uri":  {s.facebook.RedirectURL},
		"response_type": {"code"},
		"scope":         {"email,public_profile"},
		"state":         {state},
		// Ask for the email again if the person unticked it on an earlier attempt — without
		// it the dialog silently keeps the earlier refusal and errFacebookNoEmail repeats.
		"auth_type": {"rerequest"},
	}
	http.Redirect(w, r, facebookDialogURL+"?"+q.Encode(), http.StatusFound)
}

// handleFacebookCallback serves `GET /v1/auth/facebook/callback`. Failures redirect to
// `/signin?error=facebook`, with the detail in the log — except the two a person can act on
// (no email shared, email already has an account), which get their own error codes.
func (s *Server) handleFacebookCallback(w http.ResponseWriter, r *http.Request) {
	if !s.facebook.enabled() {
		http.NotFound(w, r)
		return
	}
	fail := func(msg string, err error) {
		s.log.Warn("facebook sign-in failed: "+msg, "err", err)
		code := "facebook"
		switch {
		case errors.Is(err, errFacebookNoEmail):
			code = "facebook_no_email"
		case errors.Is(err, errFacebookEmailInUse):
			code = "facebook_email_in_use"
		}
		http.Redirect(w, r, s.appBaseURL+"/signin?error="+code, http.StatusFound)
	}

	code, _, tz, err := s.readOAuthCallback(w, r, facebookCookiePath)
	if err != nil {
		fail("callback", err)
		return
	}
	ctx := r.Context()
	profile, err := s.fetchFacebookProfile(ctx, code)
	if err != nil {
		fail("profile", err)
		return
	}
	userID, created, err := s.resolveFacebookUser(ctx, profile, tz)
	if err != nil {
		fail("account resolution", err)
		return
	}
	if created && !s.skipEmailVerification {
		if err := s.sendVerificationEmail(ctx, userID, profile.Email, requestLang(r)); err != nil {
			// Not fatal, as in createAccount: the account can ask for a resend.
			s.log.Error("verification email failed", "err", err)
		}
	}
	if err := s.finishOAuthSignIn(w, r, userID); err != nil {
		fail("session start", err)
	}
}

// facebookProfile is the subset of the Graph API's /me this flow reads. ID is the app-scoped
// user id — stable for this app, different from the one any other app sees.
type facebookProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// fetchFacebookProfile trades the authorization code for an access token and reads the
// profile with it. The token came straight from Facebook's token endpoint over TLS, in
// exchange for this app's secret, so it needs no debug_token check; it is used for this one
// call and then discarded — nothing here calls Facebook on the person's behalf later.
func (s *Server) fetchFacebookProfile(ctx context.Context, code string) (facebookProfile, error) {
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := s.facebookGet(ctx, "/oauth/access_token?"+url.Values{
		"client_id":     {s.facebook.AppID},
		"client_secret": {s.facebook.AppSecret},
		"redirect_uri":  {s.facebook.RedirectURL},
		"code":          {code},
	}.Encode(), "", &tok); err != nil {
		return facebookProfile{}, fmt.Errorf("token exchange: %w", err)
	}
	if tok.AccessToken == "" {
		return facebookProfile{}, errors.New("token exchange: no access_token")
	}

	var p facebookProfile
	if err := s.facebookGet(ctx, "/me?"+url.Values{
		"fields":          {"id,name,email"},
		"appsecret_proof": {facebookAppSecretProof(tok.AccessToken, s.facebook.AppSecret)},
	}.Encode(), tok.AccessToken, &p); err != nil {
		return facebookProfile{}, fmt.Errorf("/me: %w", err)
	}
	if p.ID == "" {
		return facebookProfile{}, errors.New("/me: no id")
	}
	if p.Email == "" {
		return facebookProfile{}, errFacebookNoEmail
	}
	email, err := normalizeEmail(p.Email)
	if err != nil {
		return facebookProfile{}, fmt.Errorf("%w: %v", errFacebookNoEmail, err)
	}
	p.Email = email
	return p, nil
}

// facebookGet GETs a Graph API path (with its query) and decodes a 200 response into out.
// accessToken, when set, goes in the Authorization header rather than the URL, so it never
// lands in a proxy's access log.
func (s *Server) facebookGet(ctx context.Context, pathAndQuery, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.facebook.graphURL+pathAndQuery, nil)
	if err != nil {
		return err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp, err := s.facebook.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graph API returned %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

// facebookAppSecretProof is the Graph API's appsecret_proof: HMAC-SHA256 of the access token,
// keyed with the app secret, hex-encoded — so a leaked token alone can't be replayed against
// this app's Graph API calls (with "Require App Secret" on in the app's settings).
func facebookAppSecretProof(accessToken, appSecret string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(accessToken))
	return hex.EncodeToString(mac.Sum(nil))
}

// resolveFacebookUser maps a Facebook identity to a HoldMyTrack account, in one transaction:
//
//  1. An account already linked to this Facebook id signs in — whatever its email is now.
//  2. Otherwise, if any account already has this email, errFacebookEmailInUse: nothing is
//     linked (see this file's header for why).
//  3. Otherwise a new account is created, linked, and — unless SkipEmailVerification —
//     unverified; created reports this case so the caller sends the verification email.
func (s *Server) resolveFacebookUser(ctx context.Context, p facebookProfile, tz string) (userID string, created bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider = 'facebook' AND subject = $1`, p.ID).Scan(&userID)
	if err == nil {
		return userID, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}

	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, p.Email).Scan(&taken); err != nil {
		return "", false, err
	}
	if taken {
		return "", false, errFacebookEmailInUse
	}

	if tz == "" {
		tz = "UTC"
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, email_verified, display_name, timezone)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, p.Email, s.skipEmailVerification, newAccountDisplayName(p.Name), tz).Scan(&userID); err != nil {
		return "", false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, 'facebook', $2)`, userID, p.ID); err != nil {
		return "", false, err
	}
	return userID, true, tx.Commit(ctx)
}
