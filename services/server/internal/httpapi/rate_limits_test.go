package httpapi

// Covers docs/SPEC.md FR-1.1, FR-1.12, FR-3.6 and FR-4.15 (rate limits).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HoldMyTrack/platform/services/server/internal/export"
)

func TestRateKey(t *testing.T) {
	if got := rateKey(authInfo{userID: "u1"}, "203.0.113.7"); got != "u1" {
		t.Errorf("an account: %q, want its id", got)
	}
	if got := rateKey(authInfo{userID: DemoCustomerUserID, isDemo: true}, "203.0.113.7"); got != "ip:203.0.113.7" {
		t.Errorf("the demo: %q, want the visitor's address", got)
	}
}

func TestExportAvailableFrom(t *testing.T) {
	if got := exportAvailableFrom(nil); !got.IsZero() {
		t.Errorf("never exported: %v, want now", got)
	}
	ready := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	if got, want := exportAvailableFrom(&ready), ready.Add(export.Lifetime); !got.Equal(want) {
		t.Errorf("after a build: %v, want %v", got, want)
	}
}

// Past the minute's limit an account's tiles are refused with a Retry-After; another
// account's, and a second demo visitor's, still come.
func TestTilesPastTheMinuteLimitAreRefused(t *testing.T) {
	d := newDBTest(t)
	d.srv.SetTilesPerMinute(2)
	me, other, demo := d.newAccount(false), d.newAccount(false), d.newAccount(true)
	const path = "/tiles/v1/bike-paths/9/0/0.mvt"
	for i := range 2 {
		if rec := d.do(me, "GET", path, nil); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("tile %d refused", i+1)
		}
	}
	rec := d.do(me, "GET", path, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("third tile: status %d, Retry-After %q; want 429 with one", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := d.do(other, "GET", path, nil); rec.Code == http.StatusTooManyRequests {
		t.Fatal("another account's tile refused")
	}

	demoTile := func(ip string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Authorization", bearerPrefix+demo.session)
		rec := serveChecked(t, d.srv.ServeHTTP, req)
		return rec.Code
	}
	for range 2 {
		demoTile("203.0.113.1")
	}
	if code := demoTile("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("a demo visitor past the limit: status %d, want 429", code)
	}
	if code := demoTile("203.0.113.2"); code == http.StatusTooManyRequests {
		t.Fatal("a second demo visitor refused for the first one's tiles")
	}
}

// Past the hour's limit a phone sync is refused before its body is read, naming the limit.
func TestSyncPastTheHourlyLimitIsRefused(t *testing.T) {
	d := newDBTest(t)
	d.srv.syncLimiter = newFixedWindowLimiter(1, time.Hour)
	me := d.newAccount(false)
	if rec := d.do(me, "POST", "/v1/sync/activities", map[string]any{"activities": []any{}}); rec.Code == http.StatusTooManyRequests {
		t.Fatal("first sync refused")
	}
	rec := d.do(me, "POST", "/v1/sync/activities", map[string]any{"activities": []any{}})
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "at most 1") {
		t.Fatalf("second sync: status %d, body %q; want 429 naming the limit", rec.Code, rec.Body.String())
	}
}

// A download can be asked for once a week, counted from the last one built: not again the
// day after, at once after a failed build, and again a week on.
func TestExportOnceAWeek(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	a := d.newAccount(false)
	setLastReady := func(ago time.Duration) {
		if _, err := d.pool.Exec(ctx, `UPDATE users SET last_export_ready_at = NOW() - make_interval(secs => $2) WHERE id = $1`,
			a.id, ago.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	failPending := func() {
		if _, err := d.pool.Exec(ctx, `UPDATE jobs SET state = 'failed' WHERE user_id = $1 AND kind = 'export'`, a.id); err != nil {
			t.Fatal(err)
		}
	}

	// Never built: a failed request doesn't start the week.
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, nil)
	failPending()
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, nil)
	failPending()

	setLastReady(24 * time.Hour)
	rec := d.do(a, "POST", "/v1/account/export", nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "once a week") {
		t.Fatalf("a day after a build: status %d, body %q; want 429", rec.Code, rec.Body.String())
	}

	setLastReady(8 * 24 * time.Hour)
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, nil)
}

func TestFixedWindowAllowN(t *testing.T) {
	l := newFixedWindowLimiter(5, time.Hour)
	for _, step := range []struct {
		n    int
		want bool
	}{{3, true}, {3, false}, {2, true}, {1, false}} {
		if got := l.allowN("a", step.n); got != step.want {
			t.Errorf("allowN(%d) = %v, want %v", step.n, got, step.want)
		}
	}
	if !l.allowN("b", 5) {
		t.Error("another key shares the first one's count")
	}
}

// Synced activities are limited per hour beside sync requests: a batch that doesn't fit is
// refused whole and counts nothing, so a smaller one still fits.
func TestSyncActivitiesPastTheHourlyLimitAreRefused(t *testing.T) {
	d := newDBTest(t)
	d.srv.syncActivityLimiter = newFixedWindowLimiter(3, time.Hour)
	me := d.newAccount(false)
	batch := func(ids ...string) *httptest.ResponseRecorder {
		var acts []map[string]any
		for _, id := range ids {
			acts = append(acts, map[string]any{"external_id": id, "activity_type": "walk", "points": []map[string]any{
				{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
				{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
			}})
		}
		return d.do(me, "POST", "/v1/sync/activities", map[string]any{"source": "healthconnect", "activities": acts})
	}
	if rec := batch("a", "b"); rec.Code != http.StatusOK {
		t.Fatalf("first batch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := batch("c", "d"); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "at most 3") {
		t.Fatalf("batch past the limit: %d %q, want 429 naming it", rec.Code, rec.Body.String())
	}
	if rec := batch("c"); rec.Code != http.StatusOK {
		t.Fatalf("batch that still fits: %d %s", rec.Code, rec.Body.String())
	}
}

// One address creates at most the signup limit's accounts an hour, by the JSON endpoint and
// the page alike; a signup that fails (a taken address) doesn't count.
func TestSignupsPastTheHourlyLimitAreRefused(t *testing.T) {
	d := newDBTest(t)
	d.srv.SetSignupsPerHour(2)
	ctx := context.Background()
	prefix := fmt.Sprintf("signup-limit-%d-", time.Now().UnixNano())
	t.Cleanup(func() {
		d.pool.Exec(context.Background(), `DELETE FROM users WHERE email LIKE $1`, prefix+"%")
	})
	signup := func(email string) int {
		body := strings.NewReader(fmt.Sprintf(`{"email": %q, "password": "a long enough password"}`, email))
		req := httptest.NewRequest("POST", "/v1/auth/signup", body)
		req.Header.Set("Content-Type", "application/json")
		rec := serveChecked(t, d.srv.ServeHTTP, req)
		return rec.Code
	}
	page := func(email string) int {
		form := url.Values{"email": {email}, "password": {"a long enough password"}}
		req := httptest.NewRequest("POST", "/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://app.example")
		rec := serveChecked(t, d.srv.ServeHTTP, req)
		return rec.Code
	}
	if code := signup(prefix + "1@holdmytrack.invalid"); code != http.StatusCreated {
		t.Fatalf("first signup: %d", code)
	}
	if code := signup(prefix + "1@holdmytrack.invalid"); code != http.StatusConflict {
		t.Fatalf("taken address: %d, want 409", code)
	}
	if code := page(prefix + "2@holdmytrack.invalid"); code != http.StatusSeeOther {
		t.Fatalf("second signup, by the page: %d", code)
	}
	if code := signup(prefix + "3@holdmytrack.invalid"); code != http.StatusTooManyRequests {
		t.Fatalf("third signup: %d, want 429", code)
	}
	if code := page(prefix + "3@holdmytrack.invalid"); code != http.StatusTooManyRequests {
		t.Fatalf("third signup, by the page: %d, want 429", code)
	}
	var n int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email LIKE $1`, prefix+"%").Scan(&n); err != nil || n != 2 {
		t.Fatalf("accounts created: %d, %v; want 2", n, err)
	}
}
