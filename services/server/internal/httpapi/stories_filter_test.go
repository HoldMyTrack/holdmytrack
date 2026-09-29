package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// `story` narrows the activity list, the summary, the histogram (both modes and `earliest`)
// and the tracks tiles to one Story's activities, alongside the filters they already take.
func TestStoryFilter(t *testing.T) {
	d := newDBTest(t)
	me, other := d.newAccount(false), d.newAccount(false)
	here := [2]float64{-81.60, 41.30}
	elsewhere := [2]float64{-81.20, 41.10}
	walk := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 3000, durationSecs: 1800, at: &here,
		startedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)})
	outside := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 1000, durationSecs: 600, at: &elsewhere,
		startedAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)})
	drive := d.newActivity(me, testActivity{activityType: "driving", distanceMeters: 40000, durationSecs: 2400, at: &here,
		startedAt: time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)})
	theirs := d.newActivity(other, testActivity{activityType: "walking", distanceMeters: 500, durationSecs: 300, at: &here,
		startedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)})

	var trip, empty, theirStory story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Trip", "activity_ids": []string{walk, drive}}), http.StatusCreated, &trip)
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Empty"}), http.StatusCreated, &empty)
	d.decode(d.do(other, "POST", "/v1/stories", map[string]any{"name": "Theirs", "activity_ids": []string{theirs}}), http.StatusCreated, &theirStory)

	list := func(query string) []string {
		t.Helper()
		var resp activitiesResponse
		d.decode(d.do(me, "GET", "/v1/activities?"+query, nil), http.StatusOK, &resp)
		ids := []string{}
		for _, a := range resp.Activities {
			ids = append(ids, a.ID)
		}
		return ids
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{drive, outside, walk}},
		{"story=" + trip.ID, []string{drive, walk}},
		{"story=" + trip.ID + "&types=walking", []string{walk}},
		{"story=" + trip.ID + "&from=2026-05-02&to=2026-05-03", []string{drive}},
		{"story=" + empty.ID, []string{}},
		{"story=" + theirStory.ID, []string{}},
	} {
		if got := list(tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("list ?%s = %v, want %v", tc.query, got, tc.want)
		}
	}

	var summary activitySummaryResponse
	d.decode(d.do(me, "GET", "/v1/activities/summary?story="+trip.ID, nil), http.StatusOK, &summary)
	if summary.Count != 2 || summary.DistanceMeters != 43000 {
		t.Errorf("summary %+v, want the Story's two activities", summary)
	}

	days := func(query string) ([]string, string) {
		t.Helper()
		var resp activityHistogramResponse
		d.decode(d.do(me, "GET", "/v1/activities/histogram?"+query, nil), http.StatusOK, &resp)
		out := []string{}
		for _, b := range resp.Buckets {
			out = append(out, b.Date)
		}
		return out, resp.Earliest
	}
	for _, tc := range []struct {
		query, earliest string
		want            []string
	}{
		{"days=10", "2026-05-01", []string{"2026-05-01", "2026-05-02", "2026-05-03"}},
		{"days=10&story=" + trip.ID, "2026-05-01", []string{"2026-05-01", "2026-05-03"}},
		{"days=10&before=2026-05-03&story=" + trip.ID, "2026-05-01", []string{"2026-05-01"}},
		{"from=2026-05-01&to=2026-05-31&story=" + trip.ID, "2026-05-01", []string{"2026-05-01", "2026-05-03"}},
		{"days=10&story=" + empty.ID, "", []string{}},
		{"days=10&story=" + theirStory.ID, "", []string{}},
	} {
		got, earliest := days(tc.query)
		if !slices.Equal(got, tc.want) || earliest != tc.earliest {
			t.Errorf("histogram ?%s = %v earliest %q, want %v earliest %q", tc.query, got, earliest, tc.want, tc.earliest)
		}
	}

	const z = 12
	tile := func(at [2]float64, query string) int {
		t.Helper()
		x, y := tilemath.LonLatToTile(at[0], at[1], z)
		rec := d.do(me, "GET", fmt.Sprintf("/tiles/v1/tracks/%d/%d/%d.mvt?%s", z, x, y, query), nil)
		d.decode(rec, http.StatusOK, nil)
		return rec.Body.Len()
	}
	if tile(here, "story="+trip.ID) == 0 {
		t.Errorf("the Story's tile is empty")
	}
	if tile(elsewhere, "") == 0 {
		t.Errorf("the unfiltered tile elsewhere is empty")
	}
	for _, query := range []string{"story=" + trip.ID, "story=" + empty.ID} {
		if n := tile(elsewhere, query); n != 0 {
			t.Errorf("tile elsewhere ?%s has %d bytes; nothing there is in the Story", query, n)
		}
	}
	// Their Story's activity is right here, but it isn't mine to draw.
	if n := tile(here, "story="+theirStory.ID); n != 0 {
		t.Errorf("another account's Story drew %d bytes", n)
	}

	for _, path := range []string{"/v1/activities", "/v1/activities/summary", "/v1/activities/histogram?days=5&", "/tiles/v1/tracks/12/0/0.mvt"} {
		sep := "?"
		if path[len(path)-1] == '&' {
			sep = ""
		}
		if rec := d.do(me, "GET", path+sep+"story=nope", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed story: status %d, want 400", path, rec.Code)
		}
	}
}

// A change to which activities a Story holds bumps the account's tile version, since the
// tracks tiles take a `story` filter; anything else about a Story doesn't.
func TestStoryMembershipBumpsTileVersion(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	a := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60})
	b := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60})
	version := func() int64 {
		t.Helper()
		var v int64
		if err := d.pool.QueryRow(context.Background(), `SELECT map_version FROM users WHERE id = $1`, me.id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	var empty, st story
	steps := []struct {
		name   string
		bumps  bool
		method string
		path   func() string
		body   any
		status int
		out    *story
	}{
		{"create empty", false, "POST", func() string { return "/v1/stories" }, map[string]any{"name": "Empty"}, http.StatusCreated, &empty},
		{"create with an activity", true, "POST", func() string { return "/v1/stories" }, map[string]any{"name": "Trip", "activity_ids": []string{a}}, http.StatusCreated, &st},
		{"rename", false, "PATCH", func() string { return "/v1/stories/" + st.ID }, map[string]any{"name": "Renamed", "description": "New"}, http.StatusOK, nil},
		{"add a member again", false, "POST", func() string { return "/v1/stories/" + st.ID + "/activities" }, map[string]any{"activity_ids": []string{a}}, http.StatusOK, nil},
		{"add", true, "POST", func() string { return "/v1/stories/" + st.ID + "/activities" }, map[string]any{"activity_ids": []string{b}}, http.StatusOK, nil},
		{"remove a non-member", false, "DELETE", func() string { return "/v1/stories/" + empty.ID + "/activities" }, map[string]any{"activity_ids": []string{a}}, http.StatusOK, nil},
		{"remove", true, "DELETE", func() string { return "/v1/stories/" + st.ID + "/activities" }, map[string]any{"activity_ids": []string{b}}, http.StatusOK, nil},
		{"delete an empty Story", false, "DELETE", func() string { return "/v1/stories/" + empty.ID }, nil, http.StatusNoContent, nil},
		{"delete a Story with activities", true, "DELETE", func() string { return "/v1/stories/" + st.ID }, nil, http.StatusNoContent, nil},
	}
	for _, step := range steps {
		before := version()
		var out any
		if step.out != nil {
			out = step.out
		}
		d.decode(d.do(me, step.method, step.path(), step.body), step.status, out)
		if got := version() - before; (got > 0) != step.bumps {
			t.Errorf("%s: tile version moved by %d, want a bump: %v", step.name, got, step.bumps)
		}
	}
}
