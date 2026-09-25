package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// Email+password auth, server-side sessions (migrations/0006_sessions.sql) — resolved toward
// the smallest thing that removes the old placeholder-user stand-in, with no vendor to depend
// on, the same bias Path 3 uploads already made. Sign in with Google (google_auth.go) is an
// optional second way into the same accounts and the same sessions, off unless configured —
// email+password never depends on it. Password reset (IMPLEMENTATION.md §4.11) and email verification (docs/SPEC.md FR-1.8)
// share the same token-table shape. Rate limiting is partially built: see demoLimiter/forgotPasswordLimiter
// below, added specifically because their endpoints are reachable with no credentials at all.

const sessionCookieName = "holdmytrack_session"

// sessionTTL is deliberately long — a single-user personal app with no "remember me"
// checkbox should just stay signed in, not force a re-login every few hours.
const sessionTTL = 30 * 24 * time.Hour

// demoSessionTTL bounds how long a no-signup demo session (VISION.md §8.2) stays signed in
// before needing a fresh `POST /v1/auth/demo` call — the persistent DemoCustomerUserID
// (demo_presets.go) it points at never itself expires or gets purged, only this session
// cookie does. Long enough to try the app and share a link same day.
const demoSessionTTL = 24 * time.Hour

// passwordResetTTL is deliberately much shorter than sessionTTL/demoSessionTTL — the
// industry-standard expectation for a reset link is short-lived, since it's usually emailed
// somewhere less secure than the session cookie it's standing in for.
const passwordResetTTL = time.Hour

// emailVerificationTTL is longer than passwordResetTTL — confirming a new signup is less
// time-sensitive than a credential reset, and someone might reasonably not check their inbox
// for a day.
const emailVerificationTTL = 24 * time.Hour

const minPasswordLength = 8

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Optional — the browser's own Intl.DateTimeFormat().resolvedOptions().timeZone, sent
	// only by handleSignup's caller (decodeAuthRequest itself is shared with login, which
	// ignores this field). Invalid or absent falls back to "UTC" rather than rejecting the
	// signup over it — auto-detection is a convenience default, not something worth blocking
	// account creation over (see migrations/0021_user_timezone.sql's own doc comment).
	Timezone string `json:"timezone"`
}

// normalizeTimezone validates raw against the IANA tz database via time.LoadLocation. Returns
// ok=false for empty or unloadable input; the caller decides what that means for its own
// endpoint (handleSignup falls back to "UTC", handleUpdateSettings rejects with a 400).
func normalizeTimezone(raw string) (tz string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if _, err := time.LoadLocation(raw); err != nil {
		return "", false
	}
	return raw, true
}

// authResponse is the one shape every auth endpoint and handleMe returns — see
// loadAuthResponse, the single place that builds one, for why every endpoint returns the
// full current profile rather than just whatever fields it happened to touch.
type authResponse struct {
	// Empty for a demo account — its email is an internal, never-shown placeholder (see
	// handleDemoStart), not something to leak into a response the frontend might display.
	Email  string `json:"email"`
	IsDemo bool   `json:"isDemo"`
	// Always true for a demo account (the gate never applies to one — see requireVerified);
	// for a real account, whatever users.email_verified actually holds. The frontend's own
	// gate logic still checks IsDemo itself rather than trusting this alone, so a demo session
	// is never accidentally read as "needs to verify."
	EmailVerified bool `json:"email_verified"`
	// The Settings page's own fields (services/server/internal/httpapi/account.go).
	// DisplayName/Country/AvatarURL are "" when unset — not omitted — so the frontend never
	// has to distinguish "absent" from "empty."
	DisplayName string `json:"display_name"`
	Country     string `json:"country"`
	AvatarURL   string `json:"avatar_url"`
	// IANA name (e.g. "America/New_York"), never empty — unlike DisplayName/Country there is
	// no "unset" state: the column is NOT NULL DEFAULT 'UTC' (migrations/0021_user_timezone.sql).
	Timezone string `json:"timezone"`
}

// authResponseWithSession wraps authResponse with the freshly minted session id, for the
// four endpoints that call startSession and therefore have a session to hand back — signup,
// login, demo-start, reset-password. handleMe returns plain authResponse: it's "is my
// existing credential still valid," not a place to reissue one.
type authResponseWithSession struct {
	authResponse
	// SessionToken is the same value already set as the holdmytrack_session cookie in this same
	// response — a browser client can ignore this field entirely, it already has the
	// credential via Set-Cookie. A native client with no shared cookie jar (or one that needs
	// to inject the credential into MapLibre Native's own tile requests, which bypass the
	// app's own HTTP client — apps/android/docs/ARCHITECTURE.md §1.1's bearer-token
	// decision) stores this and sends it back as `Authorization: Bearer <SessionToken>`.
	SessionToken string `json:"session_token"`
}

// loadAuthResponse reads userID's current row and builds the shared authResponse shape —
// signup, login, demo-start and reset-password all call this instead of hand-building a
// response from just the fields they happened to already have, so a freshly created account
// reports its real column defaults (timezone, everything else unset) rather than
// a response that looks like every profile field was explicitly cleared.
func (s *Server) loadAuthResponse(ctx context.Context, userID string) (authResponse, error) {
	var email, displayName, country, avatarKey, timezone string
	var avatarUpdatedAt *time.Time
	var demoExpiresAt *time.Time
	var emailVerified bool
	err := s.pool.QueryRow(ctx, `
		SELECT email, demo_expires_at, COALESCE(display_name, ''), COALESCE(country, ''),
		       COALESCE(avatar_key, ''), avatar_updated_at, email_verified, timezone
		FROM users WHERE id = $1
	`, userID).Scan(&email, &demoExpiresAt, &displayName, &country, &avatarKey, &avatarUpdatedAt, &emailVerified, &timezone)
	if err != nil {
		return authResponse{}, err
	}
	isDemo := demoExpiresAt != nil
	if isDemo {
		email = ""           // never leak the internal, synthetic demo email — see handleDemoStart
		emailVerified = true // the gate never applies to a demo account — see requireVerified
	}
	var avatarURL string
	if avatarKey != "" && avatarUpdatedAt != nil {
		// ?v= is a cache-busting param, not a real query param the endpoint reads — it just
		// needs to change whenever the underlying image does, so a browser that cached the
		// previous avatar at the old URL never mistakes it for the new one.
		avatarURL = fmt.Sprintf("/v1/account/avatar?v=%d", avatarUpdatedAt.Unix())
	}
	return authResponse{
		Email:         email,
		IsDemo:        isDemo,
		EmailVerified: emailVerified,
		DisplayName:   displayName,
		Country:       country,
		AvatarURL:     avatarURL,
		Timezone:      timezone,
	}, nil
}

func decodeAuthRequest(r *http.Request) (authRequest, error) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return authRequest{}, errors.New("invalid request body")
	}
	return req, nil
}

// validateCredentials is the email+password check signup and sign-in share, JSON or form:
// the normalized email, or a 400 accountFailure saying what's wrong.
func validateCredentials(rawEmail, password string) (string, error) {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return "", accountFailure(http.StatusBadRequest, err.Error())
	}
	if len(password) < minPasswordLength {
		return "", accountFailure(http.StatusBadRequest, fmt.Sprintf("password must be at least %d characters", minPasswordLength))
	}
	return email, nil
}

// accountError is a failure the person should see — wrong password, a taken email, an expired
// link — as opposed to an infrastructure error, which is logged and shown only as "internal
// error". The account cores above return one; the JSON handlers turn it into a status and
// plain-text body (writeAccountError), the pages into a message on the form (auth_pages.go).
type accountError struct {
	status int
	msg    string
}

func (e *accountError) Error() string { return e.msg }

func accountFailure(status int, msg string) error { return &accountError{status: status, msg: msg} }

// writeAccountError is the JSON side of an account core's error: its own status and message
// for an accountError, a logged 500 for anything else.
func (s *Server) writeAccountError(w http.ResponseWriter, op string, err error) {
	var ae *accountError
	if errors.As(err, &ae) {
		http.Error(w, ae.msg, ae.status)
		return
	}
	s.log.Error(op+" failed", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// writeSession starts a session for userID and answers with the account plus its token —
// what every JSON endpoint that signs someone in (signup, login, demo, reset, verify) returns.
func (s *Server) writeSession(w http.ResponseWriter, r *http.Request, userID string, ttl time.Duration, status int) {
	sessionID, err := s.startSession(w, r.Context(), userID, ttl)
	if err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.loadAuthResponse(r.Context(), userID)
	if err != nil {
		s.log.Error("session response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, status, authResponseWithSession{authResponse: resp, SessionToken: sessionID})
}

// normalizeEmail is decodeAuthRequest's own validation, pulled out so handleForgotPassword
// (which has no password field to validate alongside it) can reuse it without duplicating
// the trim/lowercase/parse sequence.
func normalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(strings.ToLower(raw))
	if _, err := mail.ParseAddress(email); err != nil {
		return "", errors.New("invalid email address")
	}
	return email, nil
}

var errEmailTaken = errors.New("email already registered")

// handleSignup serves `POST /v1/auth/signup`. Always a plain new account, whether or not the
// caller currently holds a demo session — docs/ROADMAP.md's "Email verification + demo
// without real ingest" retired the old claim-the-demo-account-in-place path along with demo's
// real upload/sync access: every demo session shares the one persistent DemoCustomerUserID
// (demo_presets.go) rather than holding a row of its own, so there is nothing per-session to
// preserve by reusing it. The new account starts unverified (email_verified defaults false) and
// gets a verification email; requireVerified is what actually gates on that.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAuthRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID, err := s.createAccount(r.Context(), req.Email, req.Password, req.Timezone)
	if err != nil {
		s.writeAccountError(w, "signup", err)
		return
	}
	s.writeSession(w, r, userID, sessionTTL, http.StatusCreated)
}

// createAccount is signup's shared core — handleSignup (JSON) and the /signup page
// (auth_pages.go) both call it. email and password are validated here, not by the caller.
// timezone is the browser's own guess and falls back to "UTC" when absent or unknown, rather
// than failing the signup over a convenience default (migrations/0021_user_timezone.sql).
func (s *Server) createAccount(ctx context.Context, rawEmail, password, timezone string) (string, error) {
	email, err := validateCredentials(rawEmail, password)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	tz, ok := normalizeTimezone(timezone)
	if !ok {
		tz = "UTC"
	}
	var userID string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, email_verified, timezone) VALUES ($1, $2, $3, $4) RETURNING id`,
		email, hash, s.skipEmailVerification, tz,
	).Scan(&userID)
	if isUniqueViolation(err) {
		return "", accountFailure(http.StatusConflict, "an account with this email already exists")
	}
	if err != nil {
		return "", err
	}
	if !s.skipEmailVerification {
		if err := s.sendVerificationEmail(ctx, userID, email); err != nil {
			// Not fatal to the signup itself — the account exists and can request a resend
			// (resendVerification) — but worth knowing about if Mailgun/SMTP is down.
			s.log.Error("verification email failed", "err", err)
		}
	}
	return userID, nil
}

// sendVerificationEmail creates a fresh token and emails the verification link — called from
// handleSignup, handleChangeEmail, and handleResendVerification, the three places a real
// account needs one (re)sent.
func (s *Server) sendVerificationEmail(ctx context.Context, userID, email string) error {
	expiresAt := time.Now().Add(emailVerificationTTL)
	var tokenID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO email_verifications (user_id, expires_at) VALUES ($1, $2) RETURNING id`,
		userID, expiresAt,
	).Scan(&tokenID); err != nil {
		return err
	}

	link := fmt.Sprintf("%s/verify?token=%s", s.appBaseURL, tokenID)
	body := fmt.Sprintf(
		"Welcome to HoldMyTrack! Confirm this email address to unlock your account:\n\n%s\n\n"+
			"This link works once and expires in 24 hours. If you didn't create a HoldMyTrack "+
			"account, you can safely ignore this email.",
		link,
	)
	return s.mailer.Send(ctx, email, "Verify your HoldMyTrack email", body)
}

// handleLogin serves `POST /v1/auth/login`.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAuthRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID, err := s.checkPassword(r.Context(), req.Email, req.Password)
	if err != nil {
		s.writeAccountError(w, "login", err)
		return
	}
	s.writeSession(w, r, userID, sessionTTL, http.StatusOK)
}

// checkPassword is sign-in's shared core (handleLogin and the /signin page): the account id
// for a matching email and password. One generic failure whether the email doesn't exist,
// belongs to an unclaimed seed row (password_hash still NULL), or the password just doesn't
// match — distinguishing any of those would tell a caller which emails are registered.
func (s *Server) checkPassword(ctx context.Context, rawEmail, password string) (string, error) {
	email, err := validateCredentials(rawEmail, password)
	if err != nil {
		return "", err
	}
	var userID string
	var hash []byte
	err = s.pool.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&userID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, pgx.ErrNoRows) || hash == nil || bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return "", accountFailure(http.StatusUnauthorized, "invalid email or password")
	}
	return userID, nil
}

// handleLogout serves `POST /v1/auth/logout` — deletes the session server-side (not just
// clearing the cookie), so a captured-but-not-yet-expired token stops working immediately.
// Reads the session id via sessionIDFromRequest (cookie or bearer token), not r.Cookie
// directly, so a mobile caller presenting only Authorization: Bearer actually revokes its
// session here instead of this silently no-op'ing and clearing a cookie that was never set.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// endSession deletes the caller's session row and clears its cookie — shared by the JSON
// logout above and the pages' Sign out form (pages.go's handleLogoutPage).
func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	if sessionID, ok := sessionIDFromRequest(r); ok {
		if _, err := s.pool.Exec(r.Context(), `DELETE FROM sessions WHERE id = $1`, sessionID); err != nil {
			s.log.Error("session delete failed", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// handleMe serves `GET /v1/auth/me` — how the frontend learns whether a session cookie it's
// already holding is still valid, and whose it is, on first load (App.tsx).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.currentUserID(r)
	if !ok {
		http.Error(w, "not signed in", http.StatusUnauthorized)
		return
	}
	resp, err := s.loadAuthResponse(r.Context(), userID)
	if err != nil {
		s.log.Error("me lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDemoStart serves `POST /v1/auth/demo` — VISION.md §8.2's "no-signup,
// drag-a-file-in, see-your-fog-map page," revised per docs/ROADMAP.md's "Email verification +
// demo without real ingest": a demo account no longer gets real upload/sync access at all
// (requireNotDemo rejects those regardless of what a client attempts). Every visitor's session
// here is pointed at the same persistent, pre-seeded DemoCustomerUserID (demo_presets.go) —
// not a fresh row created and re-ingested per visitor, which would be far too slow at that
// account's real activity-history size. handleSignup never reuses this row (a demo account
// can't become a real one in place), so many concurrent demo sessions safely sharing one
// read-only user id is the whole point. Rate-limited per IP: this is the one auth endpoint
// reachable with no session or credentials at all, so it's the one auth.go's own "no rate
// limiting" gap couldn't be left alone — cheap now that it only creates a session row, but
// still worth capping.
func (s *Server) handleDemoStart(w http.ResponseWriter, r *http.Request) {
	if err := allowDemo(r); err != nil {
		s.writeAccountError(w, "demo start", err)
		return
	}
	s.writeSession(w, r, DemoCustomerUserID, demoSessionTTL, http.StatusCreated)
}

// allowDemo is the demo start's one check, shared with the page's `POST /demo`.
func allowDemo(r *http.Request) error {
	if !demoLimiter.allow(clientIP(r)) {
		return accountFailure(http.StatusTooManyRequests, "too many demo sessions from this address; try again later")
	}
	return nil
}

// demoLimiter caps how many demo accounts one address can create — 5 per hour is generous
// for a real visitor trying the app (one demo session covers a whole session's uploads) and
// cheap to hit for anything scripted, without pulling in a rate-limiting library for the one
// endpoint that needs it.
var demoLimiter = newFixedWindowLimiter(5, time.Hour)

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// handleForgotPassword serves `POST /v1/auth/forgot-password`. Always responds 200 with the
// same generic body whether or not the email matches a real, claimed account — the same
// "don't leak which emails are registered" reasoning handleLogin already applies; a failure
// sending the actual email isn't surfaced to the caller either, for the same reason. Rate
// limited per IP like handleDemoStart: the other endpoint reachable with no credentials at
// all that has a real-world side effect (sending mail) a script could otherwise abuse.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.requestPasswordReset(r, req.Email); err != nil {
		s.writeAccountError(w, "forgot-password", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists for that email, a reset link is on its way.",
	})
}

// requestPasswordReset is the forgot-password core (the JSON endpoint and the /forgot page).
// It only ever fails on the rate limit or a malformed address: an unknown email, and a failure
// actually sending the mail, both look like success to the caller.
func (s *Server) requestPasswordReset(r *http.Request, rawEmail string) error {
	if !forgotPasswordLimiter.allow(clientIP(r)) {
		return accountFailure(http.StatusTooManyRequests, "too many requests; try again later")
	}
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return accountFailure(http.StatusBadRequest, err.Error())
	}
	if err := s.sendPasswordReset(r.Context(), email); err != nil {
		s.log.Error("forgot-password failed", "err", err)
	}
	return nil
}

// sendPasswordReset looks up a real, claimed account — one with a password or a linked Google
// identity (a Google-only account sets its first password this way, docs/SPEC.md FR-1.5);
// excludes the unclaimed placeholder row and demo accounts, whose email is an internal
// placeholder nobody can type in anyway — and, if one exists, creates a token and emails the reset link. A no-op
// for an unmatched email: handleForgotPassword responds the same way either way, so there is
// nothing to report back here except a genuine infrastructure failure.
func (s *Server) sendPasswordReset(ctx context.Context, email string) error {
	var userID string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM users
		WHERE email = $1 AND demo_expires_at IS NULL AND (password_hash IS NOT NULL OR google_sub IS NOT NULL)
	`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(passwordResetTTL)
	var tokenID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO password_resets (user_id, expires_at) VALUES ($1, $2) RETURNING id`,
		userID, expiresAt,
	).Scan(&tokenID); err != nil {
		return err
	}

	link := fmt.Sprintf("%s/reset?token=%s", s.appBaseURL, tokenID)
	body := fmt.Sprintf(
		"Someone requested a password reset for this HoldMyTrack account.\n\n"+
			"Reset it here (expires in 1 hour, and only works once):\n%s\n\n"+
			"If you didn't request this, you can safely ignore this email.",
		link,
	)
	return s.mailer.Send(ctx, email, "Reset your HoldMyTrack password", body)
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleResetPassword serves `POST /v1/auth/reset-password`. A generic error for a missing or
// expired token, same reasoning as everywhere else here — distinguishing "no such token" from
// "expired" would tell a caller more than it needs to know. On success: sets the new password,
// retires every outstanding reset token for the account (not just the one used — a reset
// should close every other still-live link too), ends every existing session (this is exactly
// the moment an already-compromised one should stop working), and starts a fresh one for the
// browser completing the reset, same as signup/login already do.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	userID, err := s.resetPassword(r.Context(), req.Token, req.Password)
	if err != nil {
		s.writeAccountError(w, "reset-password", err)
		return
	}
	s.writeSession(w, r, userID, sessionTTL, http.StatusOK)
}

// resetPassword is the reset core (the JSON endpoint and the /reset page): sets the new
// password and returns the account id, for the caller to start the fresh session.
func (s *Server) resetPassword(ctx context.Context, token, password string) (string, error) {
	if len(password) < minPasswordLength {
		return "", accountFailure(http.StatusBadRequest, fmt.Sprintf("password must be at least %d characters", minPasswordLength))
	}
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM password_resets WHERE id = $1 AND expires_at > NOW()`, token,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
		return "", accountFailure(http.StatusBadRequest, "this reset link is invalid or has expired")
	}
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
		s.log.Error("reset token cleanup failed", "err", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		s.log.Error("session cleanup failed", "err", err)
	}
	return userID, nil
}

// forgotPasswordLimiter mirrors demoLimiter — 5 requests per hour per address is generous for
// someone genuinely locked out (a reset link stays usable for an hour, so retrying the
// request itself should be rare) and cheap to hit for a script trying to mail-bomb an
// arbitrary address through this endpoint.
var forgotPasswordLimiter = newFixedWindowLimiter(5, time.Hour)

type verifyEmailRequest struct {
	Token string `json:"token"`
}

// handleVerifyEmail serves `POST /v1/auth/verify-email` — the link handleSignup/
// handleChangeEmail/handleResendVerification email out. Token-based like
// handleResetPassword, not session-based: the link has to work whether or not the browser
// opening it already holds a session (a different device, a different browser profile), so it
// mints a fresh session on success exactly as reset-password does, rather than requiring one
// to already exist. A generic error for a missing or expired token, same "don't tell a caller
// more than it needs to know" reasoning as reset-password.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	userID, err := s.verifyEmail(r.Context(), req.Token)
	if err != nil {
		s.writeAccountError(w, "verify-email", err)
		return
	}
	s.writeSession(w, r, userID, sessionTTL, http.StatusOK)
}

// verifyEmail is the verification core (the JSON endpoint and the /verify page): marks the
// token's account verified and returns its id, for the caller to start a session.
func (s *Server) verifyEmail(ctx context.Context, token string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM email_verifications WHERE id = $1 AND expires_at > NOW()`, token,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
		return "", accountFailure(http.StatusBadRequest, "this verification link is invalid or has expired")
	}
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET email_verified = true WHERE id = $1`, userID); err != nil {
		return "", err
	}
	// Every outstanding token for this account, not just the one used — mirrors
	// resetPassword's own "close every other still-live link too" reasoning, applied to a
	// resend that arrived after this one was already clicked.
	if _, err := s.pool.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID); err != nil {
		s.log.Error("verification token cleanup failed", "err", err)
	}
	return userID, nil
}

// handleResendVerification serves `POST /v1/auth/resend-verification` — plain requireAuth,
// deliberately not requireVerified, since the whole point is helping an account that hasn't
// verified yet. Rate-limited per account rather than per IP (resendVerificationLimiter): the
// caller is already an authenticated user at this point, not an anonymous one, so the abuse
// case this guards is one signed-in account spamming its own inbox, not a script targeting
// arbitrary addresses.
func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	info := authInfoFromContext(r.Context())
	if info.emailVerified && !info.isDemo {
		writeJSON(w, http.StatusOK, map[string]string{"message": "This account is already verified."})
		return
	}
	if err := s.resendVerification(r.Context(), info); err != nil {
		s.writeAccountError(w, "resend verification", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Verification email sent."})
}

// resendVerification is the resend core (the JSON endpoint and the /verify-pending page).
func (s *Server) resendVerification(ctx context.Context, info authInfo) error {
	if info.isDemo {
		return accountFailure(http.StatusBadRequest, "demo accounts have no email to verify")
	}
	if !resendVerificationLimiter.allow(info.userID) {
		return accountFailure(http.StatusTooManyRequests, "too many requests; try again later")
	}
	var email string
	if err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, info.userID).Scan(&email); err != nil {
		return err
	}
	return s.sendVerificationEmail(ctx, info.userID, email)
}

// resendVerificationLimiter mirrors forgotPasswordLimiter's shape but keys on the account's
// own id rather than an IP — see handleResendVerification's own doc comment for why.
var resendVerificationLimiter = newFixedWindowLimiter(5, time.Hour)

type changeEmailRequest struct {
	Email string `json:"email"`
}

// handleChangeEmail serves `PATCH /v1/auth/email` — plain requireAuth like
// handleResendVerification, reachable before verification specifically so a mistyped signup
// email can be corrected (docs/ROADMAP.md: "resend alone doesn't help someone who typed the
// address wrong in the first place"). Any change resets email_verified to false and sends a
// fresh verification email to the new address, whether or not the account was already
// verified — an unconfirmed address is unconfirmed regardless of how it got there.
func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	info := authInfoFromContext(r.Context())
	var req changeEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.changeEmail(r.Context(), info, req.Email); err != nil {
		s.writeAccountError(w, "change-email", err)
		return
	}
	resp, err := s.loadAuthResponse(r.Context(), info.userID)
	if err != nil {
		s.log.Error("change-email response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// changeEmail is the change-email core (the JSON endpoint and the /verify-pending page).
func (s *Server) changeEmail(ctx context.Context, info authInfo, rawEmail string) error {
	if info.isDemo {
		return accountFailure(http.StatusBadRequest, "demo accounts have no email to change")
	}
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return accountFailure(http.StatusBadRequest, err.Error())
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE users SET email = $2, email_verified = false WHERE id = $1`, info.userID, email,
	); err != nil {
		if isUniqueViolation(err) {
			return accountFailure(http.StatusConflict, "an account with this email already exists")
		}
		return err
	}
	// Old tokens pointed at a verification link that would still verify an address this
	// account no longer holds, if the new address ever unluckily collided with a stale one.
	if _, err := s.pool.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, info.userID); err != nil {
		s.log.Error("verification token cleanup failed", "err", err)
	}
	if err := s.sendVerificationEmail(ctx, info.userID, email); err != nil {
		s.log.Error("change-email verification send failed", "err", err)
	}
	return nil
}

type fixedWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	counts map[string]*windowCount
}

type windowCount struct {
	count      int
	windowFrom time.Time
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, window: window, counts: make(map[string]*windowCount)}
}

// allow also sweeps every stale entry on each call, so the map stays bounded by recently
// active callers rather than growing forever from one-off addresses that never return —
// the only cleanup this needs, since demo starts are inherently infrequent per caller.
func (l *fixedWindowLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for k, c := range l.counts {
		if now.Sub(c.windowFrom) > l.window {
			delete(l.counts, k)
		}
	}

	c, ok := l.counts[key]
	if !ok {
		l.counts[key] = &windowCount{count: 1, windowFrom: now}
		return true
	}
	if c.count >= l.limit {
		return false
	}
	c.count++
	return true
}

// clientIP takes r.RemoteAddr's host part rather than trusting X-Forwarded-For — no reverse
// proxy sits in front of this server today (compose.yaml publishes api's port directly), so
// that header would just be an unverified value any caller could set to defeat demoLimiter
// entirely. Revisit the day a proxy actually terminates connections in front of this.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// startSession returns the new session id, alongside setting it as the holdmytrack_session
// cookie — a browser client needs nothing more, but the four callers that mint a fresh
// session (signup, login, demo-start, reset-password) also thread this value into
// authResponseWithSession.SessionToken, apps/android/docs/ARCHITECTURE.md §1.1's
// bearer-token path for a native client with no browser-style cookie handling.
func (s *Server) startSession(w http.ResponseWriter, ctx context.Context, userID string, ttl time.Duration) (string, error) {
	expiresAt := time.Now().Add(ttl)
	var sessionID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, expires_at) VALUES ($1, $2) RETURNING id`,
		userID, expiresAt,
	).Scan(&sessionID); err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		// Derived from APP_BASE_URL rather than a separate env var — that's already the one
		// place this server is told whether it's serving over HTTPS (it's also what a
		// password-reset link is built from, sendPasswordReset). Local dev's default
		// ("http://localhost:5173") keeps this false, matching the plain-HTTP dev server;
		// the deploy runbook (docs/DEPLOY.md) sets a real https:// APP_BASE_URL in
		// production, which is what flips this on.
		Secure:   strings.HasPrefix(s.appBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
	return sessionID, nil
}

// bearerPrefix is RFC 6750's scheme name for the Authorization header value —
// sessionIDFromRequest strips exactly this before treating the remainder as a session id.
const bearerPrefix = "Bearer "

// sessionIDFromRequest reads the session id a caller presented: the holdmytrack_session cookie
// for a browser (checked first — the long-established, higher-volume path), falling back to
// `Authorization: Bearer <session-id>` for a native client. This is not a second credential
// system — it's the same sessions.id value, just presented a second way, because a mobile
// app needs to inject it into MapLibre Native's own tile requests via a per-request header
// hook (apps/android/docs/ARCHITECTURE.md §1.1's bearer-token decision), which a cookie
// sitting in the app's own cookie jar wouldn't reach.
func sessionIDFromRequest(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, bearerPrefix) {
		if token := strings.TrimPrefix(auth, bearerPrefix); token != "" {
			return token, true
		}
	}
	return "", false
}

// currentUserID resolves the caller's session id (sessionIDFromRequest) to a user id, or
// ("", false) if there is none, or it doesn't match a live session. Expiry is checked in SQL
// (expires_at > NOW()), not left to the browser dropping an expired cookie on its own — a
// client could replay an old one past its stated expiry.
func (s *Server) currentUserID(r *http.Request) (string, bool) {
	sessionID, ok := sessionIDFromRequest(r)
	if !ok {
		return "", false
	}
	var userID string
	err := s.pool.QueryRow(r.Context(),
		`SELECT user_id FROM sessions WHERE id = $1 AND expires_at > NOW()`, sessionID,
	).Scan(&userID)
	if err != nil {
		return "", false
	}
	return userID, true
}

type contextKey string

const authContextKey contextKey = "authInfo"

// authInfo is what requireAuth resolves a session down to, in one query, and attaches to the
// request context — userID for every handler that used to read the old PlaceholderUserID
// constant, isDemo/emailVerified for the two gates layered on top (requireVerified,
// requireNotDemo), and timezone for handlers that need the account's own setting
// (day-bucketing) without a second round trip each.
type authInfo struct {
	userID        string
	isDemo        bool
	emailVerified bool
	timezone      string
}

func authInfoFromContext(ctx context.Context) authInfo {
	info, _ := ctx.Value(authContextKey).(authInfo)
	return info
}

// userIDFromContext reads the user id requireAuth already resolved and attached to the
// request context — every handler that used to read the PlaceholderUserID constant reads
// this instead now.
func userIDFromContext(ctx context.Context) string {
	return authInfoFromContext(ctx).userID
}

// timezoneFromContext reads the authenticated user's stored IANA timezone name — the raw
// string a day-bucketing query passes as its own `AT TIME ZONE $N` parameter.
func timezoneFromContext(ctx context.Context) string {
	return authInfoFromContext(ctx).timezone
}

// locationFromContext resolves timezoneFromContext to a *time.Location, for the Go-side date
// math (time.ParseInLocation, time.Now().In(loc)) day-bucketing queries need alongside their
// own SQL-side AT TIME ZONE parameter. Falls back to UTC on a load failure — defensive only,
// since every stored value was already validated with time.LoadLocation before being written
// (handleSignup, handleUpdateSettings), so this should never actually fail in practice.
func locationFromContext(ctx context.Context) *time.Location {
	loc, err := time.LoadLocation(timezoneFromContext(ctx))
	if err != nil {
		return time.UTC
	}
	return loc
}

// requireAuth wraps a handler that needs an authenticated user, threading the resolved
// authInfo through the request context rather than changing every wrapped handler's own
// signature — the mechanical diff (replacing PlaceholderUserID with
// userIDFromContext(r.Context()) at each call site) is far smaller than re-plumbing a new
// parameter through every handler and every server.go registration. Deliberately the only one
// of the three auth middlewares that never rejects on isDemo/emailVerified — used directly by
// handleResendVerification and handleChangeEmail, which exist specifically to help an account
// that hasn't verified yet.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, ok := sessionIDFromRequest(r)
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		var info authInfo
		err := s.pool.QueryRow(r.Context(), `
			SELECT u.id, u.demo_expires_at IS NOT NULL, u.email_verified, u.timezone
			FROM sessions se JOIN users u ON u.id = se.user_id
			WHERE se.id = $1 AND se.expires_at > NOW()
		`, sessionID).Scan(&info.userID, &info.isDemo, &info.emailVerified, &info.timezone)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), authContextKey, info)))
	}
}

// requireVerified wraps requireAuth with docs/ROADMAP.md's email-verification gate: a real
// (non-demo) account whose email isn't yet confirmed gets a distinguishable 403 instead of
// reaching the handler, so the frontend can render the "verify your email" screen rather than
// a generic error. Demo accounts always pass — the gate never applied to them; see
// handleDemoStart's own doc comment for why a demo account has no email worth verifying at
// all. This is the middleware every previously-plain-requireAuth route below gets, except the
// handful (resend-verification, change-email) that exist to help an unverified account.
func (s *Server) requireVerified(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		info := authInfoFromContext(r.Context())
		if !info.isDemo && !info.emailVerified {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "email_not_verified"})
			return
		}
		next(w, r)
	})
}

// requireNotDemo layers on top of requireVerified for the write endpoints a demo account must
// never reach — upload, sync, edit, delete, avatar, settings — docs/ROADMAP.md: "Demo Users
// can only view preset activities and can't do any modifications." A real, verified account
// passes through unchanged; a demo account gets a distinguishable 403 a frontend can turn into
// "create an account to save your own data" rather than a generic error.
func (s *Server) requireNotDemo(next http.HandlerFunc) http.HandlerFunc {
	return s.requireVerified(func(w http.ResponseWriter, r *http.Request) {
		if authInfoFromContext(r.Context()).isDemo {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "demo_read_only",
				"message": "Demo accounts can't add, edit, or delete activities — create an account to save your own data.",
			})
			return
		}
		next(w, r)
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isInvalidTextRepresentation catches a token that isn't even a UUID — a mangled link — which
// Postgres rejects outright rather than matching no row; it's still just an invalid link.
func isInvalidTextRepresentation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
