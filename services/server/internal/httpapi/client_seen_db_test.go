package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An authenticated request marks the client it came from: the website, or the Android app with
// its build. The app's WebView marks neither.
func TestRequestsMarkTheirClient(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	get := func(agent string, inApp bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/coverage/status", nil)
		req.Header.Set("Authorization", bearerPrefix+me.session)
		req.Header.Set("User-Agent", agent)
		if inApp {
			req.AddCookie(&http.Cookie{Name: appCookieName, Value: "android"})
		}
		rec := httptest.NewRecorder()
		d.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	read := func() (web, app *time.Time, build *int) {
		t.Helper()
		if err := d.pool.QueryRow(context.Background(),
			`SELECT web_seen_at, android_seen_at, android_build FROM users WHERE id = $1`, me.id).Scan(&web, &app, &build); err != nil {
			t.Fatal(err)
		}
		return web, app, build
	}

	get("Mozilla/5.0 (Linux; Android 16; wv) Chrome/140.0", true)
	if web, app, _ := read(); web != nil || app != nil {
		t.Fatalf("the app's WebView marked web %v, app %v; want neither", web, app)
	}
	get("Mozilla/5.0 (Macintosh) Firefox/140.0", false)
	if web, app, _ := read(); web == nil || app != nil {
		t.Fatalf("a browser marked web %v, app %v; want only web", web, app)
	}
	get("HoldMyTrack-Android/0.14 (9537c263; 1255)", false)
	if _, app, build := read(); app == nil || build == nil || *build != 1255 {
		t.Fatalf("the app marked %v with build %v, want today with 1255", app, build)
	}
	// An update later the same day is seen at once.
	get("HoldMyTrack-Android/0.15 (aaaaaaaa; 1260)", false)
	if _, _, build := read(); build == nil || *build != 1260 {
		t.Fatalf("after an update the build is %v, want 1260", build)
	}
}
