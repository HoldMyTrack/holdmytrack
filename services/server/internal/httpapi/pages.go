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
// Signed out, that makes the whole page the same for everyone, so it's served from
// RenderPublic's cache, cacheable; signed in, it's rendered fresh with the account's header.
// canonicalPath is for a page that shares its content with another URL (About with `/`).
func (s *Server) staticPage(page, title, description, canonicalPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := web.PageData{Title: title, Description: description, Path: r.URL.Path, CanonicalPath: canonicalPath}
		if data.User = s.pageUser(r); data.User == nil {
			s.pages.RenderPublic(w, page, data)
			return
		}
		s.pages.Render(w, http.StatusOK, page, data)
	}
}

// homeTitle and homeDescription are the site's own — the signed-out home page's, and About's,
// which is the same page (appShell).
const homeTitle = "HoldMyTrack — Every journey, mapped."
const homeDescription = "HoldMyTrack is a free, community-funded place to see every outdoor activity you have ever recorded on one map — Fog of War, heatmaps and routes from your watch, phone or old exports. No subscription, no ads, no data sales."

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

// appShell serves the React app — the map, at `/`, the one page that isn't rendered here
// (ADR-0012). Only for a session that has something to show: no session gets the signed-out
// home page instead (About's content), an unverified real account to
// /verify-pending, one that has never saved Settings to /settings (pageAccount.home). The
// React app's own copies of the first two checks (App.tsx) stay as a fallback for a session
// that ends while the page is open.
func (s *Server) appShell(title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dest := legacyLanding(r); dest != "" {
			http.Redirect(w, r, dest, http.StatusSeeOther)
			return
		}
		acct := s.pageAccount(r)
		// Signed out, `/` is the site's front page — About's content, not a redirect to a
		// sign-in form, since `/` is the URL people type, share and search engines rank.
		if acct == nil {
			s.pages.RenderPublic(w, "about", web.PageData{Title: homeTitle, Description: homeDescription, Path: "/"})
			return
		}
		if home := acct.home(); home != "/" {
			http.Redirect(w, r, home, http.StatusSeeOther)
			return
		}
		s.pages.RenderApp(w, web.AppPage{
			PageData: web.PageData{Title: title, Path: r.URL.Path, NoIndex: true, User: acct.user},
			// Only a dev server honours this, and only for a request Vite's dev proxy marked
			// (apps/web/vite.config.ts); `vite preview` and production get the built bundle.
			ViteDev: s.pages.Dev() && r.Header.Get(viteDevHeader) == "dev",
		})
	}
}

// viteDevHeader is how Vite's dev proxy tells the app shell to load the bundle from Vite's
// dev server rather than from /assets/ — the same name as apps/web/vite.config.ts's.
const viteDevHeader = "X-HoldMyTrack-Vite"

// legacyLanding forwards the links emails carried before the auth screens became pages
// (`/?reset_token=`, `/?verify_token=`) and the old Google-failure redirect
// (`/?auth_error=`) to the page that handles each now. Keep while such emails may still be
// sitting in inboxes; a reset link lives an hour, a verification link a day.
func legacyLanding(r *http.Request) string {
	if r.URL.Path != "/" {
		return ""
	}
	q := r.URL.Query()
	switch {
	case q.Get("reset_token") != "":
		return "/reset?token=" + url.QueryEscape(q.Get("reset_token"))
	case q.Get("verify_token") != "":
		return "/verify?token=" + url.QueryEscape(q.Get("verify_token"))
	case q.Get("auth_error") != "":
		return "/signin?error=google"
	}
	return ""
}

// notFound answers every path nothing else claims: an HTML page for a browser, a plain 404
// under the API's own prefixes, where a client expects no page.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, apiPrefix+"/") || strings.HasPrefix(r.URL.Path, "/tiles/") {
		http.NotFound(w, r)
		return
	}
	s.pages.Render(w, http.StatusNotFound, "notfound", web.PageData{Title: "Page not found — HoldMyTrack", Path: r.URL.Path, NoIndex: true, User: s.pageUser(r)})
}
