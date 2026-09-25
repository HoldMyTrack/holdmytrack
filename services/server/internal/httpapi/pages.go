package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The server-rendered pages (ADR-0012, IMPLEMENTATION.md §4.19): internal/web renders them,
// this file decides who is asking. Unlike the /v1 JSON routes, a page never answers 401 — a
// signed-out visitor gets the page with a "Sign in" link where the account menu would be.
// The auth pages themselves — sign in, sign up, reset, verify — are in auth_pages.go.

// pageUser is the optional-auth lookup every page does: the signed-in account as the header
// shows it, or nil for no session (or an expired one). Unlike requireAuth it never rejects,
// and an unverified account still counts as signed in — the header only needs a name.
// (auth_pages.go's pageAccount is the same lookup with the session's flags kept.)
func (s *Server) pageUser(r *http.Request) *web.User {
	if acct := s.pageAccount(r); acct != nil {
		return acct.user
	}
	return nil
}

// staticPage serves a page whose content is the same for everyone — only the header differs.
func (s *Server) staticPage(page, title, description string, noIndex bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.pages.Render(w, http.StatusOK, page, web.PageData{
			Title: title, Description: description, Path: r.URL.Path, NoIndex: noIndex, User: s.pageUser(r),
		})
	}
}

// handleLogoutPage serves `POST /logout` — the header's Sign out form. The same revocation as
// `POST /v1/auth/logout` (endSession), then a redirect to the map rather than a 204.
func (s *Server) handleLogoutPage(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// sameOrigin guards the pages' form POSTs against cross-site request forgery. The JSON API
// never needed this: a cross-site form can't send a JSON body or a PATCH/DELETE, and the
// session cookie's SameSite=Lax already keeps it off cross-site subrequests. A plain
// urlencoded form POST is exactly what a hostile page *can* send, so page forms also require
// the browser's Origin header (or, from an older browser that omits it, Referer) to be this
// app's own origin (APP_BASE_URL). A request that carries neither is refused too.
func (s *Server) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isSameOrigin(r) {
			http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) isSameOrigin(r *http.Request) bool {
	want, err := url.Parse(s.appBaseURL)
	if err != nil || want.Host == "" {
		return false
	}
	wantOrigin := want.Scheme + "://" + want.Host
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == wantOrigin
	}
	referer := r.Header.Get("Referer")
	return referer == wantOrigin || strings.HasPrefix(referer, wantOrigin+"/")
}
