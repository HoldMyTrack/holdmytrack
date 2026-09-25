package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

func TestAdminPagesSignedOutAreNotFound(t *testing.T) {
	s := newPagesTestServer(t)
	for _, path := range []string{"/admin", "/admin/users/b123028b-354d-4838-bb84-b31213269791"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Page not found") {
			t.Errorf("%s: status %d, want the ordinary 404 page", path, rec.Code)
		}
	}
}

func TestAdminPagesRender(t *testing.T) {
	s := newPagesTestServer(t)
	admin := &web.User{Email: "admin@example.com", IsAdmin: true}
	user := adminUserRow{ID: "u-1", Email: "rider@example.com", DisplayName: "Rider", SignedUp: "2026-08-01", Timezone: "Europe/Rome", Verified: true, Activities: "2", Distance: "12 km", First: "2026-08-02", Last: "2026-09-20"}

	rec := httptest.NewRecorder()
	s.pages.Render(rec, http.StatusOK, "admin", web.PageData{Title: "Admin", Path: "/admin", NoIndex: true, User: admin, Page: adminUsersView{Users: []adminUserRow{user}}})
	body := rec.Body.String()
	for _, want := range []string{`href="/admin/users/u-1"`, "rider@example.com", "Europe/Rome", `href="/admin">Admin</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page: missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	s.pages.Render(rec, http.StatusOK, "admin-user", web.PageData{Title: "Rider", Path: "/admin/users/u-1", NoIndex: true, User: admin, Page: adminUserView{
		User:     user,
		Range:    "1–2 of 2",
		NextHref: "/admin/users/u-1?page=2",
		Activities: []adminActivityRow{
			{ID: "a-1", Started: "2026-09-20 08:15", Type: "walk", Countries: "Italy", Regions: "Emilia-Romagna", Edited: true},
			{ID: "a-2", Started: "2026-09-19 18:00", Type: "walk", SupersededBy: "a-1", Hidden: true},
		},
	}})
	body = rec.Body.String()
	for _, want := range []string{`<code class="admin__id">a-1</code>`, "Emilia-Romagna", `href="#a-1"`, ">hidden<", ">edited<", "1–2 of 2", `href="/admin/users/u-1?page=2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("admin user page: missing %q", want)
		}
	}
}

func TestAdminMenuItemOnlyForAdmins(t *testing.T) {
	s := newPagesTestServer(t)
	for _, tc := range []struct {
		user *web.User
		want bool
	}{
		{&web.User{Email: "a@example.com", IsAdmin: true}, true},
		{&web.User{Email: "b@example.com"}, false},
	} {
		rec := httptest.NewRecorder()
		s.pages.Render(rec, http.StatusOK, "help", web.PageData{Title: "Help", Path: "/help", User: tc.user})
		if got := strings.Contains(rec.Body.String(), `href="/admin"`); got != tc.want {
			t.Errorf("%s: Admin item shown = %v, want %v", tc.user.Email, got, tc.want)
		}
	}
}

func TestAdminFormatting(t *testing.T) {
	for secs, want := range map[int64]string{0: "0:00", 29: "0:00", 30: "0:01", 2520: "0:42", 43500: "12:05"} {
		if got := adminDuration(secs); got != want {
			t.Errorf("adminDuration(%d) = %q, want %q", secs, got, want)
		}
	}
	if got := adminRange(100, 100, 1250); got != "101–200 of 1,250" {
		t.Errorf("adminRange = %q", got)
	}
	if got := adminUserHref("u", 1); got != "/admin/users/u" {
		t.Errorf("page 1 href = %q", got)
	}
	if got := adminUserHref("u", 3); got != "/admin/users/u?page=3" {
		t.Errorf("page 3 href = %q", got)
	}
}
