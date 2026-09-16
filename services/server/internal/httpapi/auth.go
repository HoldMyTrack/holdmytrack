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

// Simple email+password auth, server-side sessions (migrations/0006_sessions.sql) — resolved
// toward the smallest thing that removes PlaceholderUserID, not a third-party identity
// provider: no vendor to depend on, the same bias Path 3 uploads already made. No email
// verification — a real gap for a multi-user deployment, not built here because this is
// explicitly the *simple* version. Password reset (IMPLEMENTATION.md §4.11) is
// built, once internal/mail existed to build it on. Rate limiting is partially built: see
// demoLimiter/forgotPasswordLimiter below, added specifically because their endpoints are
// reachable with no credentials at all.

const sessionCookieName = "fitmap_session"

// sessionTTL is deliberately long — a single-user personal app with no "remember me"
// checkbox should just stay signed in, not force a re-login every few hours.
const sessionTTL = 30 * 24 * time.Hour

// demoSessionTTL bounds how long a no-signup demo account (VISION.md §8.2) and its
// uploaded data live before internal/worker's purge sweep removes them — long enough to try
// the app and share a link same day, short enough to bound storage from anonymous traffic
// that never converts to a real account.
const demoSessionTTL = 24 * time.Hour

// passwordResetTTL is deliberately much shorter than sessionTTL/demoSessionTTL — the
// industry-standard expectation for a reset link is short-lived, since it's usually emailed
// somewhere less secure than the session cookie it's standing in for.
const passwordResetTTL = time.Hour

const minPasswordLength = 8

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authResponse is the one shape every auth endpoint and handleMe returns — see
// loadAuthResponse, the single place that builds one, for why every endpoint returns the
// full current profile rather than just whatever fields it happened to touch.
type authResponse struct {
	// Empty for a demo account — its email is an internal, never-shown placeholder (see
	// handleDemoStart), not something to leak into a response the frontend might display.
	Email  string `json:"email"`
	IsDemo bool   `json:"isDemo"`
	// The Settings page's own fields (services/server/internal/httpapi/account.go).
	// DisplayName/Country/AvatarURL are "" when unset — not omitted — so the frontend never
	// has to distinguish "absent" from "empty," and PrivacyTrimM is never omitted either,
	// since 0 (trimming turned off) is a real, meaningful value, not an absent one.
	DisplayName  string `json:"display_name"`
	Country      string `json:"country"`
	AvatarURL    string `json:"avatar_url"`
	PrivacyTrimM int    `json:"privacy_trim_m"`
}

// authResponseWithSession wraps authResponse with the freshly minted session id, for the
// four endpoints that call startSession and therefore have a session to hand back — signup,
// login, demo-start, reset-password. handleMe returns plain authResponse: it's "is my
// existing credential still valid," not a place to reissue one.
type authResponseWithSession struct {
	authResponse
	// SessionToken is the same value already set as the fitmap_session cookie in this same
	// response — a browser client can ignore this field entirely, it already has the
	// credential via Set-Cookie. A native client with no shared cookie jar (or one that needs
	// to inject the credential into MapLibre Native's own tile requests, which bypass the
	// app's own HTTP client — apps/android/docs/ROADMAP.md's "Decide the mobile auth
	// surface") stores this and sends it back as `Authorization: Bearer <SessionToken>`.
	SessionToken string `json:"session_token"`
}

// loadAuthResponse reads userID's current row and builds the shared authResponse shape —
// signup, login, demo-start and reset-password all call this instead of hand-building a
// response from just the fields they happened to already have, so a freshly created account
// reports its real column defaults (privacy_trim_m: 200, everything else unset) rather than
// a response that looks like every profile field was explicitly cleared.
func (s *Server) loadAuthResponse(ctx context.Context, userID string) (authResponse, error) {
	var email, displayName, country, avatarKey string
	var avatarUpdatedAt *time.Time
	var demoExpiresAt *time.Time
	var privacyTrimM int
	err := s.pool.QueryRow(ctx, `
		SELECT email, demo_expires_at, COALESCE(display_name, ''), COALESCE(country, ''),
		       COALESCE(avatar_key, ''), avatar_updated_at, privacy_trim_m
		FROM users WHERE id = $1
	`, userID).Scan(&email, &demoExpiresAt, &displayName, &country, &avatarKey, &avatarUpdatedAt, &privacyTrimM)
	if err != nil {
		return authResponse{}, err
	}
	isDemo := demoExpiresAt != nil
	if isDemo {
		email = "" // never leak the internal, synthetic demo email — see handleDemoStart
	}
	var avatarURL string
	if avatarKey != "" && avatarUpdatedAt != nil {
		// ?v= is a cache-busting param, not a real query param the endpoint reads — it just
		// needs to change whenever the underlying image does, so a browser that cached the
		// previous avatar at the old URL never mistakes it for the new one.
		avatarURL = fmt.Sprintf("/v1/account/avatar?v=%d", avatarUpdatedAt.Unix())
	}
	return authResponse{
		Email:        email,
		IsDemo:       isDemo,
		DisplayName:  displayName,
		Country:      country,
		AvatarURL:    avatarURL,
		PrivacyTrimM: privacyTrimM,
	}, nil
}

func decodeAuthRequest(r *http.Request) (authRequest, error) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return authRequest{}, errors.New("invalid request body")
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return authRequest{}, err
	}
	req.Email = email
	if len(req.Password) < minPasswordLength {
		return authRequest{}, fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	return req, nil
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

// handleSignup serves `POST /v1/auth/signup`. Three cases, in priority order:
//  1. The caller already holds a live demo session (VISION.md §8.2) — claim that
//     account in place (claimDemoUser) so whatever they uploaded during the demo survives
//     becoming a real account, rather than starting a second, empty one.
//  2. Otherwise, the very first signup ever claims the seeded placeholder user
//     (migrations/0003_seed_demo_user.sql) — see claimOrCreateUser — so activity history
//     uploaded before accounts existed stays attached to the same account.
//  3. Otherwise, a plain new account.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAuthRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.log.Error("password hash failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	var userID string
	if demoUserID, ok := s.currentDemoUserID(r); ok {
		userID, err = s.claimDemoUser(ctx, demoUserID, req.Email, hash)
	} else {
		userID, err = s.claimOrCreateUser(ctx, req.Email, hash)
	}
	if err != nil {
		if errors.Is(err, errEmailTaken) {
			http.Error(w, "an account with this email already exists", http.StatusConflict)
			return
		}
		s.log.Error("signup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionID, err := s.startSession(w, ctx, userID, sessionTTL)
	if err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("signup response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, authResponseWithSession{authResponse: resp, SessionToken: sessionID})
}

// claimOrCreateUser: if no account anywhere has ever set a password, this signup claims the
// seeded placeholder row (updating its email/password_hash in place, keeping its id and
// therefore every activity already attributed to it) instead of inserting a new one.
// Otherwise it's a plain new-account insert. "Has anyone ever set a password" rather than
// "does the placeholder row still have email = the seed's own value" so this keeps working
// correctly even if the seed migration's placeholder email was already changed by hand.
func (s *Server) claimOrCreateUser(ctx context.Context, email string, hash []byte) (string, error) {
	var anyClaimed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE password_hash IS NOT NULL)`).Scan(&anyClaimed); err != nil {
		return "", err
	}

	if !anyClaimed {
		var userID string
		err := s.pool.QueryRow(ctx, `
			UPDATE users SET email = $2, password_hash = $3
			WHERE id = $1 AND password_hash IS NULL
			RETURNING id
		`, PlaceholderUserID, email, hash).Scan(&userID)
		if err == nil {
			return userID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			if isUniqueViolation(err) {
				return "", errEmailTaken
			}
			return "", err
		}
		// No placeholder row to claim (e.g. a fresh DB without 0003's seed applied) —
		// fall through to a plain create below.
	}

	var userID string
	err := s.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`, email, hash).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return "", errEmailTaken
		}
		return "", err
	}
	return userID, nil
}

// currentDemoUserID resolves the caller's session the same way currentUserID does, but only
// returns it when that account is still an active demo (demo_expires_at IS NOT NULL) — a
// real account's own session must never be mistaken for one here.
func (s *Server) currentDemoUserID(r *http.Request) (string, bool) {
	sessionID, ok := sessionIDFromRequest(r)
	if !ok {
		return "", false
	}
	var userID string
	err := s.pool.QueryRow(r.Context(), `
		SELECT u.id FROM sessions se
		JOIN users u ON u.id = se.user_id
		WHERE se.id = $1 AND se.expires_at > NOW() AND u.demo_expires_at IS NOT NULL
	`, sessionID).Scan(&userID)
	if err != nil {
		return "", false
	}
	return userID, true
}

// claimDemoUser turns an active demo account into a real one in place — same id, so every
// activity uploaded during the demo (VISION.md §8.2) stays attached — by setting its
// email/password_hash and clearing demo_expires_at so internal/worker's purge sweep stops
// treating it as ephemeral.
func (s *Server) claimDemoUser(ctx context.Context, userID, email string, hash []byte) (string, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE users SET email = $2, password_hash = $3, demo_expires_at = NULL WHERE id = $1
	`, userID, email, hash); err != nil {
		if isUniqueViolation(err) {
			return "", errEmailTaken
		}
		return "", err
	}
	return userID, nil
}

// handleLogin serves `POST /v1/auth/login`.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAuthRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	var userID string
	var hash []byte
	err = s.pool.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, req.Email).Scan(&userID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Error("login lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// One generic response whether the email doesn't exist, belongs to an unclaimed seed
	// row (password_hash still NULL), or the password just doesn't match — distinguishing
	// any of those would tell a caller which emails are registered.
	if errors.Is(err, pgx.ErrNoRows) || hash == nil || bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	sessionID, err := s.startSession(w, ctx, userID, sessionTTL)
	if err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("login response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, authResponseWithSession{authResponse: resp, SessionToken: sessionID})
}

// handleLogout serves `POST /v1/auth/logout` — deletes the session server-side (not just
// clearing the cookie), so a captured-but-not-yet-expired token stops working immediately.
// Reads the session id via sessionIDFromRequest (cookie or bearer token), not r.Cookie
// directly, so a mobile caller presenting only Authorization: Bearer actually revokes its
// session here instead of this silently no-op'ing and clearing a cookie that was never set.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
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
	w.WriteHeader(http.StatusNoContent)
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
// drag-a-file-in, see-your-fog-map page." Creates a real user row, marked ephemeral via
// demo_expires_at, and a real session for it exactly like login/signup — the entire existing
// upload/ingest/fog/tile pipeline then runs completely unchanged for a demo visitor. The
// email is a synthetic, never-shown placeholder (a real one isn't needed until — and unless —
// claimDemoUser turns this into a real account); ".invalid" is the RFC 2606 TLD reserved for
// addresses guaranteed never to be real. Rate-limited per IP: this is the one auth endpoint
// reachable with no session or credentials at all, so it's the one auth.go's own "no rate
// limiting" gap couldn't be left alone — an anonymous endpoint that creates real rows and
// accepts uploads is a cheaper target than any of the already-authenticated ones.
func (s *Server) handleDemoStart(w http.ResponseWriter, r *http.Request) {
	if !demoLimiter.allow(clientIP(r)) {
		http.Error(w, "too many demo sessions from this address; try again later", http.StatusTooManyRequests)
		return
	}

	ctx := r.Context()
	var userID string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, demo_expires_at)
		VALUES ('demo-' || gen_random_uuid() || '@fitmap.invalid', NOW() + $1)
		RETURNING id
	`, demoSessionTTL).Scan(&userID)
	if err != nil {
		s.log.Error("demo start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionID, err := s.startSession(w, ctx, userID, demoSessionTTL)
	if err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("demo start response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, authResponseWithSession{authResponse: resp, SessionToken: sessionID})
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
	if !forgotPasswordLimiter.allow(clientIP(r)) {
		http.Error(w, "too many requests; try again later", http.StatusTooManyRequests)
		return
	}

	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.sendPasswordReset(r.Context(), email); err != nil {
		s.log.Error("forgot-password failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists for that email, a reset link is on its way.",
	})
}

// sendPasswordReset looks up a real, claimed account (password_hash IS NOT NULL — excludes
// unclaimed placeholder rows and demo accounts, whose email is an internal placeholder nobody
// can type in anyway) and, if one exists, creates a token and emails the reset link. A no-op
// for an unmatched email: handleForgotPassword responds the same way either way, so there is
// nothing to report back here except a genuine infrastructure failure.
func (s *Server) sendPasswordReset(ctx context.Context, email string) error {
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1 AND password_hash IS NOT NULL`, email).Scan(&userID)
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

	link := fmt.Sprintf("%s/?reset_token=%s", s.appBaseURL, tokenID)
	body := fmt.Sprintf(
		"Someone requested a password reset for this FitMap account.\n\n"+
			"Reset it here (expires in 1 hour, and only works once):\n%s\n\n"+
			"If you didn't request this, you can safely ignore this email.",
		link,
	)
	return s.mailer.Send(ctx, email, "Reset your FitMap password", body)
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
	if len(req.Password) < minPasswordLength {
		http.Error(w, fmt.Sprintf("password must be at least %d characters", minPasswordLength), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM password_resets WHERE id = $1 AND expires_at > NOW()`, req.Token,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "this reset link is invalid or has expired", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.log.Error("reset-password lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.log.Error("password hash failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
		s.log.Error("password update failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
		s.log.Error("reset token cleanup failed", "err", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		s.log.Error("session cleanup failed", "err", err)
	}

	sessionID, err := s.startSession(w, ctx, userID, sessionTTL)
	if err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("reset-password response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, authResponseWithSession{authResponse: resp, SessionToken: sessionID})
}

// forgotPasswordLimiter mirrors demoLimiter — 5 requests per hour per address is generous for
// someone genuinely locked out (a reset link stays usable for an hour, so retrying the
// request itself should be rare) and cheap to hit for a script trying to mail-bomb an
// arbitrary address through this endpoint.
var forgotPasswordLimiter = newFixedWindowLimiter(5, time.Hour)

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

// startSession returns the new session id, alongside setting it as the fitmap_session
// cookie — a browser client needs nothing more, but the four callers that mint a fresh
// session (signup, login, demo-start, reset-password) also thread this value into
// authResponseWithSession.SessionToken, apps/android/docs/ROADMAP.md's "Decide the mobile
// auth surface" bearer-token path for a native client with no browser-style cookie handling.
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

// sessionIDFromRequest reads the session id a caller presented: the fitmap_session cookie
// for a browser (checked first — the long-established, higher-volume path), falling back to
// `Authorization: Bearer <session-id>` for a native client. This is not a second credential
// system — it's the same sessions.id value, just presented a second way, because a mobile
// app needs to inject it into MapLibre Native's own tile requests via a per-request header
// hook (apps/android/docs/ROADMAP.md's "Decide the mobile auth surface"), which a cookie
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

const userIDContextKey contextKey = "userID"

// userIDFromContext reads the user id requireAuth already resolved and attached to the
// request context — every handler that used to read the PlaceholderUserID constant reads
// this instead now.
func userIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(userIDContextKey).(string)
	return id
}

// requireAuth wraps a handler that needs an authenticated user, threading the resolved id
// through the request context rather than changing every wrapped handler's own signature —
// the mechanical diff (replacing PlaceholderUserID with userIDFromContext(r.Context()) at
// each call site) is far smaller than re-plumbing a new parameter through every handler and
// every server.go registration.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.currentUserID(r)
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userIDContextKey, userID)))
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
