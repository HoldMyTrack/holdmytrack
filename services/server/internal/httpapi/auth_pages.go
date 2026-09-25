package httpapi

import (
	"errors"
	"net/http"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The sign-in, sign-up, password-reset and email-verification pages (ADR-0012,
// IMPLEMENTATION.md §4.19). Each form calls the same account core its JSON endpoint does
// (auth.go: checkPassword, createAccount, resetPassword, verifyEmail, …), so the two can't
// drift; what differs is only the answer — a redirect and a cookie here, instead of JSON. A
// failed form is rendered again with the message and the email kept, at the failure's own
// status. Every form POST is behind sameOrigin.

// authForm is what the auth page templates read from PageData.Page. The handlers set
// ErrorKey/NoticeKey (catalog keys), renderAuth fills Error/Notice in the page's language.
type authForm struct {
	Error     string
	Notice    string
	ErrorKey  string
	NoticeKey string
	Email     string
	Token     string
	// Google shows "Continue with Google" — only when the server can complete that flow.
	Google bool
	// Upgrading is a demo session creating a real account (the account menu's "Create your
	// own account"): the sign-up page offers "Back to the map" instead of another demo.
	Upgrading bool
	// Sent is /forgot's "check your email" state, after its form is submitted.
	Sent bool
}

// pageAccount is the optional-auth lookup every page does: who is signed in (the header's
// account), whether it's the demo, whether the email is verified, its language. nil for no
// live session (or an expired one). Unlike requireAuth it never rejects, and an unverified
// account still counts as signed in — the header only needs a name.
type pageAccount struct {
	info    authInfo
	profile authResponse
	user    *web.User
}

func (s *Server) pageAccount(r *http.Request) *pageAccount {
	sessionID, ok := sessionIDFromRequest(r)
	if !ok {
		return nil
	}
	var info authInfo
	err := s.pool.QueryRow(r.Context(), `
		SELECT u.id, u.demo_expires_at IS NOT NULL, u.email_verified, u.timezone, COALESCE(u.locale, ''), u.is_admin
		FROM sessions se JOIN users u ON u.id = se.user_id
		WHERE se.id = $1 AND se.expires_at > NOW()
	`, sessionID).Scan(&info.userID, &info.isDemo, &info.emailVerified, &info.timezone, &info.locale, &info.isAdmin)
	if err != nil {
		return nil
	}
	resp, err := s.loadAuthResponse(r.Context(), info.userID)
	if err != nil {
		s.log.Error("page account lookup failed", "err", err)
		return nil
	}
	return &pageAccount{info: info, profile: resp, user: &web.User{Email: resp.Email, DisplayName: resp.DisplayName, AvatarURL: resp.AvatarURL, IsDemo: resp.IsDemo, IsAdmin: info.isAdmin && !info.isDemo}}
}

// pageLang is the language a page renders in for acct (nil when signed out): the account's own
// setting, else the browser's (i18n.Resolve).
func pageLang(acct *pageAccount, r *http.Request) string {
	if acct == nil {
		return i18n.Resolve("", r)
	}
	return i18n.Resolve(acct.info.locale, r)
}

// home is where a signed-in account belongs: the map; or, for a real account whose email
// isn't confirmed yet, the page that says so, since the map has nothing to show it; or, for
// one that has never saved Settings (no Country — FR-1.7's first run), Settings, since Country
// and Timezone decide how every number and day on the map reads. A demo account is never
// gated on either.
func (a *pageAccount) home() string {
	switch {
	case a.info.isDemo:
		return "/"
	case !a.info.emailVerified:
		return "/verify-pending"
	case a.profile.Country == "":
		return "/settings"
	}
	return "/"
}

// renderAuth renders one of the auth pages; titleKey is its title's catalog key. The sign-in
// page carries the site's description, for a search result or a shared link that lands on it.
// (The site's own front page is `/`, About's content.)
func (s *Server) renderAuth(w http.ResponseWriter, r *http.Request, status int, page, titleKey string, noIndex bool, form authForm) {
	s.renderAuthFailure(w, r, status, page, titleKey, noIndex, form, nil)
}

func (s *Server) renderAuthFailure(w http.ResponseWriter, r *http.Request, status int, page, titleKey string, noIndex bool, form authForm, ae *accountError) {
	var user *web.User
	acct := s.pageAccount(r)
	if acct != nil {
		user = acct.user
	}
	lang := pageLang(acct, r)
	l := i18n.Get(lang)
	switch {
	case ae != nil:
		form.Error = ae.message(lang)
	case form.ErrorKey != "":
		form.Error = l.T(form.ErrorKey)
	}
	if form.NoticeKey != "" {
		form.Notice = l.T(form.NoticeKey)
	}
	data := web.PageData{Title: l.T("meta.title", "page", l.T(titleKey)), Path: r.URL.Path, NoIndex: noIndex, User: user, Page: form, Lang: lang}
	if page == "signin" {
		data.Description = l.T("meta.site_description")
	}
	s.pages.Render(w, status, page, data)
}

// renderAuthError re-renders a form after one of the account cores failed: an accountError's
// own status and message, or a logged 500 with a generic one.
func (s *Server) renderAuthError(w http.ResponseWriter, r *http.Request, op, page, titleKey string, noIndex bool, form authForm, err error) {
	var ae *accountError
	if !errors.As(err, &ae) {
		s.log.Error(op+" failed", "err", err)
		ae = &accountError{status: http.StatusInternalServerError, key: "error.internal"}
	}
	s.renderAuthFailure(w, r, ae.status, page, titleKey, noIndex, form, ae)
}

// signIn starts a session for userID and sends the browser on to where the account belongs.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request, userID string) {
	if _, err := s.startSession(w, r.Context(), userID, sessionTTL); err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	dest := "/"
	var verified bool
	if err := s.pool.QueryRow(r.Context(), `SELECT email_verified FROM users WHERE id = $1`, userID).Scan(&verified); err == nil && !verified {
		dest = "/verify-pending"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// GET /signin. Someone already signed in goes where their account belongs instead.
func (s *Server) handleSignInPage(w http.ResponseWriter, r *http.Request) {
	if acct := s.pageAccount(r); acct != nil && !acct.info.isDemo {
		http.Redirect(w, r, acct.home(), http.StatusSeeOther)
		return
	}
	form := authForm{Google: s.google.enabled()}
	// google_auth.go's callback lands here on any failure, with no detail on purpose.
	if r.URL.Query().Get("error") == "google" {
		form.ErrorKey = "signin.google_failed"
	}
	s.renderAuth(w, r, http.StatusOK, "signin", "signin.title", false, form)
}

// POST /signin.
func (s *Server) handleSignInForm(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	userID, err := s.checkPassword(r.Context(), email, r.PostFormValue("password"))
	if err != nil {
		s.renderAuthError(w, r, "sign in", "signin", "signin.title", false, authForm{Email: email, Google: s.google.enabled()}, err)
		return
	}
	s.signIn(w, r, userID)
}

// GET /signup. A demo session is allowed here — it's how a demo becomes a real account.
func (s *Server) handleSignUpPage(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct != nil && !acct.info.isDemo {
		http.Redirect(w, r, acct.home(), http.StatusSeeOther)
		return
	}
	// noindex: /signin is the one sign-in-or-up page search results should show (it links here).
	s.renderAuth(w, r, http.StatusOK, "signup", "signup.title", true, authForm{Google: s.google.enabled(), Upgrading: acct != nil})
}

// POST /signup. timezone comes from a hidden field the page's one-line script fills from the
// browser; without it the account starts on UTC, as the JSON endpoint's would.
func (s *Server) handleSignUpForm(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	userID, err := s.createAccount(r.Context(), email, r.PostFormValue("password"), r.PostFormValue("timezone"), pageLang(s.pageAccount(r), r))
	if err != nil {
		acct := s.pageAccount(r)
		s.renderAuthError(w, r, "sign up", "signup", "signup.title", true, authForm{Email: email, Google: s.google.enabled(), Upgrading: acct != nil && acct.info.isDemo}, err)
		return
	}
	s.signIn(w, r, userID)
}

// POST /demo — the sign-in page's "Try it now — no signup".
func (s *Server) handleDemoForm(w http.ResponseWriter, r *http.Request) {
	if err := allowDemo(r); err != nil {
		s.renderAuthError(w, r, "demo start", "signin", "signin.title", false, authForm{Google: s.google.enabled()}, err)
		return
	}
	if _, err := s.startSession(w, r.Context(), DemoCustomerUserID, demoSessionTTL); err != nil {
		s.log.Error("session start failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GET /forgot.
func (s *Server) handleForgotPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuth(w, r, http.StatusOK, "forgot", "forgot.title", true, authForm{})
}

// POST /forgot. Answers "check your email" whether or not the address has an account.
func (s *Server) handleForgotForm(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	if err := s.requestPasswordReset(r, email); err != nil {
		s.renderAuthError(w, r, "forgot password", "forgot", "forgot.title", true, authForm{Email: email}, err)
		return
	}
	s.renderAuth(w, r, http.StatusOK, "forgot", "forgot.title", true, authForm{Sent: true})
}

// GET /reset?token= — the link in the password-reset email. The token is only checked on
// submit; checking it here too would just be a second query for the same answer.
func (s *Server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	form := authForm{Token: r.URL.Query().Get("token")}
	if form.Token == "" {
		form.ErrorKey = "error.reset_link_invalid"
	}
	s.renderAuth(w, r, http.StatusOK, "reset", "reset.title", true, form)
}

// POST /reset.
func (s *Server) handleResetForm(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	userID, err := s.resetPassword(r.Context(), token, r.PostFormValue("password"))
	if err != nil {
		s.renderAuthError(w, r, "reset password", "reset", "reset.title", true, authForm{Token: token}, err)
		return
	}
	s.signIn(w, r, userID)
}

// GET /verify?token= — the link in the verification email. A GET with an effect, because
// it's a link someone clicks in their inbox; the effect is idempotent (verifying twice
// verifies once, and the token is single-use), which is what makes that acceptable.
func (s *Server) handleVerifyPage(w http.ResponseWriter, r *http.Request) {
	userID, err := s.verifyEmail(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		s.renderAuthError(w, r, "verify email", "verify", "verify.title", true, authForm{}, err)
		return
	}
	s.signIn(w, r, userID)
}

// GET /verify-pending — a signed-in real account whose email isn't confirmed yet: resend the
// link, or correct a mistyped address. ?sent / ?changed are the two forms' PRG notices.
func (s *Server) handleVerifyPendingPage(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	if acct.home() != "/verify-pending" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	form := authForm{Email: acct.user.Email}
	switch {
	case r.URL.Query().Has("sent"):
		form.NoticeKey = "verify_pending.sent"
	case r.URL.Query().Has("changed"):
		form.NoticeKey = "verify_pending.changed"
	}
	s.renderAuth(w, r, http.StatusOK, "verify-pending", "verify.title", true, form)
}

// POST /verify-pending/resend.
func (s *Server) handleVerifyResendForm(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	if err := s.resendVerification(r.Context(), acct.info, pageLang(acct, r)); err != nil {
		s.renderAuthError(w, r, "resend verification", "verify-pending", "verify.title", true, authForm{Email: acct.user.Email}, err)
		return
	}
	http.Redirect(w, r, "/verify-pending?sent", http.StatusSeeOther)
}

// POST /verify-pending/email.
func (s *Server) handleVerifyChangeEmailForm(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	if err := s.changeEmail(r.Context(), acct.info, r.PostFormValue("email"), pageLang(acct, r)); err != nil {
		s.renderAuthError(w, r, "change email", "verify-pending", "verify.title", true, authForm{Email: acct.user.Email}, err)
		return
	}
	http.Redirect(w, r, "/verify-pending?changed", http.StatusSeeOther)
}
