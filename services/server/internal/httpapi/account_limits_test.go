package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

func TestFitWithinLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		u       accountUsage
		sizes   []int64
		fit     int
		refused string
	}{
		{"room for all", accountUsage{10, 1000}, []int64{5, 5, 5}, 3, ""},
		{"the last activity that fits", accountUsage{maxActivitiesPerAccount - 1, 0}, []int64{1}, 1, ""},
		{"at the activity limit", accountUsage{maxActivitiesPerAccount, 0}, []int64{1}, 0, limitActivities},
		{"part of a batch fits", accountUsage{maxActivitiesPerAccount - 2, 0}, []int64{1, 1, 1, 1}, 2, limitActivities},
		{"exactly filling storage", accountUsage{0, maxOriginalBytesPerAccount - 100}, []int64{100}, 1, ""},
		{"one byte over storage", accountUsage{0, maxOriginalBytesPerAccount - 100}, []int64{101}, 0, limitStorage},
		{"storage refuses mid-batch", accountUsage{0, maxOriginalBytesPerAccount - 100}, []int64{60, 60}, 1, limitStorage},
		{"nothing arriving", accountUsage{maxActivitiesPerAccount, maxOriginalBytesPerAccount}, nil, 0, ""},
	} {
		fit, refused := fitWithinLimits(tc.u, tc.sizes)
		if fit != tc.fit || refused != tc.refused {
			t.Errorf("%s: got %d %q, want %d %q", tc.name, fit, refused, tc.fit, tc.refused)
		}
	}
}

func TestLimitMessage(t *testing.T) {
	l := i18n.Get("en")
	if got := limitMessage(l, limitActivities); !strings.Contains(got, "50,000 activities") {
		t.Errorf("activity limit message %q", got)
	}
	if got := limitMessage(l, limitStorage); !strings.Contains(got, "5 GB") {
		t.Errorf("storage limit message %q", got)
	}
}

// fillToLimit gives the account activities up to n, in one statement.
func (d *dbTest) fillToLimit(as account, n int) {
	d.t.Helper()
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO activities (user_id, source, activity_type, started_at, timezone)
		SELECT $1, 'upload', 'walk', NOW() - make_interval(secs => g), 'UTC' FROM generate_series(1, $2) g`,
		as.id, n); err != nil {
		d.t.Fatal(err)
	}
}

// At the activity limit an upload is refused with the limit's message, before anything is
// queued; a waiting ingest job counts as an activity already.
func TestUploadRefusedAtTheActivityLimit(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.fillToLimit(me, maxActivitiesPerAccount-1)
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO jobs (kind, user_id, payload, run_after) VALUES ('ingest', $1, '{"raw_bytes": 10}', '2999-01-01')`, me.id); err != nil {
		t.Fatal(err)
	}
	rec := d.uploadRaw(me, "day.gpx", []byte(splitGPX))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "50,000 activities") {
		t.Fatalf("upload at the limit: %d %q, want 409 naming the limit", rec.Code, rec.Body.String())
	}
	var queued int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE user_id = $1`, me.id).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("jobs: %d, %v; want only the one already waiting", queued, err)
	}
}

// Stored originals count against the storage limit, each original once however many split
// pieces share it.
func TestUploadRefusedAtTheStorageLimit(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	half := int64(maxOriginalBytesPerAccount / 2)
	for range 2 { // two pieces of one original: half the limit, not all of it
		d.newActivity(me, testActivity{})
	}
	if _, err := d.pool.Exec(context.Background(),
		`UPDATE activities SET raw_payload_key = 'raw/x', raw_bytes = $2 WHERE user_id = $1`, me.id, half); err != nil {
		t.Fatal(err)
	}
	if resp := d.uploadFile(me, "day.gpx", []byte(splitGPX)); resp.Status != "enqueued" {
		t.Fatalf("upload under the limit: %q", resp.Status)
	}
	if _, err := d.pool.Exec(context.Background(),
		`UPDATE activities SET raw_bytes = $2 WHERE user_id = $1`, me.id, maxOriginalBytesPerAccount-10); err != nil {
		t.Fatal(err)
	}
	rec := d.uploadRaw(me, "other.gpx", []byte(strings.Replace(splitGPX, "2026", "2025", 1)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "5 GB") {
		t.Fatalf("upload past the storage limit: %d %q, want 409 naming the limit", rec.Code, rec.Body.String())
	}
}

// A sync batch crossing the limit takes what fits and rejects the rest, each with the
// limit's message, so the phone keeps them.
func TestSyncRejectsWhatDoesntFit(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.fillToLimit(me, maxActivitiesPerAccount-1)
	points := []map[string]any{
		{"lat": 50.0, "lon": 10.0, "time": "2026-05-01T10:00:00Z"},
		{"lat": 50.001, "lon": 10.001, "time": "2026-05-01T10:01:00Z"},
	}
	var acts []map[string]any
	for _, id := range []string{"walk-a", "walk-b", "walk-c"} {
		acts = append(acts, map[string]any{"external_id": id, "activity_type": "walk", "points": points})
	}
	var resp syncActivitiesResponse
	d.decode(d.do(me, http.MethodPost, "/v1/sync/activities", map[string]any{"source": "healthconnect", "activities": acts}), http.StatusOK, &resp)
	for i, want := range []string{"enqueued", "rejected", "rejected"} {
		if resp.Results[i].Status != want {
			t.Errorf("result %d: %s, want %s", i, resp.Results[i].Status, want)
		}
	}
	if !strings.Contains(resp.Results[2].Error, "50,000 activities") {
		t.Errorf("rejection %q, want the limit's message", resp.Results[2].Error)
	}
}

// An account at its limit can't accept a copy of a Story; the copy stays in its inbox.
func TestStoryCopyRefusedAtTheLimit(t *testing.T) {
	d := newDBTest(t)
	me, you := d.newAccount(false), d.newAccount(false)
	storyID := d.newStory(me, "Trip", d.newActivity(me, testActivity{}))
	d.decode(d.do(me, "POST", "/v1/stories/"+storyID+"/send", map[string]any{"email": d.address(you)}), http.StatusNoContent, nil)
	d.fillToLimit(you, maxActivitiesPerAccount)
	sends := d.inbox(you)
	if len(sends) != 1 {
		t.Fatalf("inbox %+v", sends)
	}
	if rec := d.do(you, "POST", "/v1/story-sends/"+sends[0].ID+"/accept", nil); rec.Code != http.StatusConflict {
		t.Fatalf("accept at the limit: %d, want 409", rec.Code)
	}
	if len(d.inbox(you)) != 1 {
		t.Error("the refused copy left the inbox")
	}
}

// Settings shows what the account holds against its limits, a waiting import included; the
// demo account, which can't add anything, doesn't see it.
func TestSettingsShowsTheAccountsLimits(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	d.fillToLimit(me, 1240)
	if _, err := d.pool.Exec(context.Background(),
		`UPDATE activities SET raw_payload_key = 'raw/' || id, raw_bytes = 250000 WHERE user_id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	body := getPage(d.srv, "/settings", me.session, false).Body.String()
	if want := "1,240 of 50,000 activities · 310 MB of 5 GB of original files"; !strings.Contains(body, want) {
		t.Errorf("/settings: %q missing", want)
	}
	demo := d.newAccount(true)
	if strings.Contains(getPage(d.srv, "/settings", demo.session, false).Body.String(), `id="account-limits"`) {
		t.Error("/settings for the demo: the limits shown")
	}
}
