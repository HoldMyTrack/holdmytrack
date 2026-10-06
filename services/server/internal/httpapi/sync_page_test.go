package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Sync page lists finished imports only — a Ready one with a link to it on the map, a
// Failed one with its reason — never one still processing, and opening it counts every
// failure so far as seen.
func TestSyncPage(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `UPDATE users SET imports_seen_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	activity := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60,
		startedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)})
	var source string
	if err := d.pool.QueryRow(ctx, `UPDATE activities SET external_id = 'walk-1' WHERE id = $1 RETURNING source`, activity).Scan(&source); err != nil {
		t.Fatal(err)
	}
	externalID := "walk-1"
	d.insertImportJob(me, "done", source, "morning-walk.gpx", "", "")
	if _, err := d.pool.Exec(ctx, `UPDATE jobs SET payload = payload || jsonb_build_object('external_id', $2::text) WHERE user_id = $1 AND payload->>'source_detail' = 'morning-walk.gpx'`, me.id, externalID); err != nil {
		t.Fatal(err)
	}
	d.insertImportJob(me, "failed", "upload", "broken.gpx", "", "")
	d.insertImportJob(me, "pending", "upload", "still-going.gpx", "", "")

	rec := d.do(me, "GET", "/sync", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sync: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"morning-walk.gpx", "broken.gpx", `href="/?activity=` + activity + `&amp;day=2026-05-02"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(body, "still-going.gpx") {
		t.Error("page lists an import still processing")
	}

	var resp activeImportsResponse
	d.decode(d.do(me, "GET", "/v1/uploads/active", nil), http.StatusOK, &resp)
	if resp.UnseenFailures != 0 {
		t.Errorf("unseen failures after opening /sync: %d, want 0", resp.UnseenFailures)
	}

	// Signed out, the page sends you to sign in.
	if rec := d.do(account{}, "GET", "/sync", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("signed out: %d, want 303", rec.Code)
	}
}

// An import whose activity was set aside as a duplicate is in the one list, marked Duplicate
// with the source of the copy kept, and has no link to the map, its activity being on none.
func TestSyncPageDuplicate(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	ctx := context.Background()
	kept := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60})
	dup := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60, supersededBy: kept})
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET source = 'healthconnect', external_id = 'hc-1' WHERE id = $1`, dup); err != nil {
		t.Fatal(err)
	}
	d.insertImportJob(me, "done", "healthconnect", "hc-1", "", "")

	body := d.do(me, "GET", "/sync", nil).Body.String()
	for _, want := range []string{"sync__row--duplicate", "Kept the copy from an uploaded file"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(body, "activity="+dup) {
		t.Error("a duplicate links to the map")
	}
}
