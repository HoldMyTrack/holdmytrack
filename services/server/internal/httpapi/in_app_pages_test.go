package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The pages a native app shows in its WebView (SPEC.md FR-10.8) carry appCookieName on every
// request, and render without the site's header, footer and Back to map.

func getPage(s *Server, path, session string, app bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if session != "" {
		req.Header.Set("Authorization", bearerPrefix+session)
	}
	if app {
		req.AddCookie(&http.Cookie{Name: appCookieName, Value: "android"})
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

const (
	siteHeader = `class="page-header"`
	siteFooter = `class="page-footer"`
	backToMap  = `class="settings__back"`
)

// A signed-out page is cached per language; the in-app copy must not be handed to a browser,
// nor the browser's to the app, whichever is asked for first.
func TestInAppPublicPageIsCachedApart(t *testing.T) {
	s := newPagesTestServer(t)
	for i, app := range []bool{true, false, true} {
		rec := getPage(s, "/about", "", app)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
		body := rec.Body.String()
		for _, chrome := range []string{siteHeader, siteFooter} {
			if strings.Contains(body, chrome) == app {
				t.Errorf("request %d (in-app %v): %s present = %v", i, app, chrome, !app)
			}
		}
		if !strings.Contains(body, `id="funding"`) {
			t.Errorf("request %d (in-app %v): About's content is missing", i, app)
		}
		// An app is signed in already: no demo button.
		if strings.Contains(body, `href="/signin"`) == app {
			t.Errorf("request %d (in-app %v): demo button present = %v", i, app, !app)
		}
	}
}

func TestInAppAccountPages(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	for _, path := range []string{"/settings", "/profile", "/sync"} {
		web := getPage(d.srv, path, me.session, false).Body.String()
		app := getPage(d.srv, path, me.session, true).Body.String()
		for _, chrome := range []string{siteHeader, siteFooter, backToMap} {
			if !strings.Contains(web, chrome) {
				t.Errorf("%s in a browser: %s missing", path, chrome)
			}
			if strings.Contains(app, chrome) {
				t.Errorf("%s in-app: %s present", path, chrome)
			}
		}
	}

	// Settings in an app is the account's fields only: the app has its own Theme, Download
	// your data and Delete account.
	web := getPage(d.srv, "/settings", me.session, false).Body.String()
	app := getPage(d.srv, "/settings", me.session, true).Body.String()
	for _, section := range []string{`class="settings__title"`, "data-theme-control", `id="download-data"`, `id="delete-account"`} {
		if !strings.Contains(web, section) {
			t.Errorf("/settings in a browser: %s missing", section)
		}
		if strings.Contains(app, section) {
			t.Errorf("/settings in-app: %s present", section)
		}
	}
	// Sync's heading is the app screen's title, and its text names no Upload menu.
	syncWeb := getPage(d.srv, "/sync", me.session, false).Body.String()
	syncApp := getPage(d.srv, "/sync", me.session, true).Body.String()
	if !strings.Contains(syncWeb, `id="sync-history"`) || !strings.Contains(syncWeb, "Upload menu") {
		t.Error("/sync in a browser: heading or Upload menu text missing")
	}
	if strings.Contains(syncApp, `id="sync-history"`) || strings.Contains(syncApp, "Upload menu") {
		t.Error("/sync in-app: heading or Upload menu text present")
	}
	for _, field := range []string{`name="display_name"`, `name="country"`, `name="timezone"`, `action="/settings/avatar"`} {
		if !strings.Contains(app, field) {
			t.Errorf("/settings in-app: %s missing", field)
		}
	}
}
