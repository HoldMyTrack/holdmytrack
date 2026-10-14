package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
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
		rec := httptest.NewRecorder()
		d.srv.ServeHTTP(rec, req)
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
