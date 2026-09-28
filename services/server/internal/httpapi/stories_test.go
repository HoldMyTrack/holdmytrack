package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func intPtr(v int) *int { return &v }

func TestStoriesLifecycle(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	walk := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 3000, movingSeconds: intPtr(1800), durationSecs: 2000,
		startedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)})
	driveThere := d.newActivity(me, testActivity{activityType: "driving", distanceMeters: 40000, movingSeconds: intPtr(2400), durationSecs: 2600,
		startedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)})
	// No moving time recorded: counts its elapsed time as moving, as Trends does.
	driveBack := d.newActivity(me, testActivity{activityType: "driving", distanceMeters: 41000, durationSecs: 2700,
		startedAt: time.Date(2026, 5, 1, 15, 0, 0, 0, time.UTC)})
	// A superseded duplicate is a member but isn't counted.
	duplicate := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 2900, movingSeconds: intPtr(1700), durationSecs: 1900,
		startedAt: time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC), supersededBy: walk})

	var created story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{
		"name": "  Brecksville  ", "description": "The reservation trip",
		"activity_ids": []string{walk, driveThere, strings.ToUpper(walk)},
	}), http.StatusCreated, &created)
	if created.Name != "Brecksville" || created.Description == nil || *created.Description != "The reservation trip" {
		t.Errorf("created %q / %v", created.Name, created.Description)
	}
	if want := []string{driveThere, walk}; !slices.Equal(created.ActivityIDs, want) {
		t.Errorf("activity_ids %v, want %v (oldest first)", created.ActivityIDs, want)
	}

	var got story
	d.decode(d.do(me, "POST", "/v1/stories/"+created.ID+"/activities", map[string]any{
		"activity_ids": []string{driveBack, duplicate, walk},
	}), http.StatusOK, &got)
	if len(got.ActivityIDs) != 4 {
		t.Errorf("after add: %d members, want 4", len(got.ActivityIDs))
	}
	if !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("adding members didn't move updated_at")
	}
	wantStats := storyStats{Count: 3, DistanceMeters: 84000, MovingSeconds: 1800 + 2400 + 2700, ElapsedSeconds: 2000 + 2600 + 2700,
		ByType: []storyTypeStats{
			{ActivityType: "driving", Count: 2, DistanceMeters: 81000, MovingSeconds: 5100, ElapsedSeconds: 5300},
			{ActivityType: "walking", Count: 1, DistanceMeters: 3000, MovingSeconds: 1800, ElapsedSeconds: 2000},
		}}
	if !storyStatsEqual(got.Stats, wantStats) {
		t.Errorf("stats %+v, want %+v", got.Stats, wantStats)
	}

	d.decode(d.do(me, "PATCH", "/v1/stories/"+created.ID, map[string]any{"name": "Brecksville Reservation", "description": ""}), http.StatusOK, &got)
	if got.Name != "Brecksville Reservation" || got.Description != nil {
		t.Errorf("after rename: %q / %v", got.Name, got.Description)
	}

	d.decode(d.do(me, "DELETE", "/v1/stories/"+created.ID+"/activities", map[string]any{
		"activity_ids": []string{duplicate, driveBack, "00000000-0000-0000-0000-000000000000"},
	}), http.StatusOK, &got)
	if want := []string{driveThere, walk}; !slices.Equal(got.ActivityIDs, want) {
		t.Errorf("after remove: %v, want %v", got.ActivityIDs, want)
	}

	var other story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Empty"}), http.StatusCreated, &other)
	var list storiesResponse
	d.decode(d.do(me, "GET", "/v1/stories", nil), http.StatusOK, &list)
	if len(list.Stories) != 2 || list.Stories[0].ID != other.ID || list.Stories[1].ID != created.ID {
		t.Fatalf("list %+v, want the two Stories newest first", list.Stories)
	}
	if list.Stories[0].ActivityIDs == nil || list.Stories[0].Stats.ByType == nil {
		t.Errorf("an empty Story's lists should be [], not null")
	}
	d.decode(d.do(me, "GET", "/v1/stories/"+created.ID, nil), http.StatusOK, &got)
	if got.Name != "Brecksville Reservation" || len(got.ActivityIDs) != 2 {
		t.Errorf("get: %+v", got)
	}

	d.decode(d.do(me, "DELETE", "/v1/stories/"+created.ID, nil), http.StatusNoContent, nil)
	d.decode(d.do(me, "GET", "/v1/stories/"+created.ID, nil), http.StatusNotFound, nil)
	// Deleting a Story deletes none of its activities.
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM activities WHERE user_id = $1`, me.id).Scan(&n); err != nil || n != 4 {
		t.Errorf("activities after deleting the Story: %d (%v), want 4", n, err)
	}
}

func storyStatsEqual(a, b storyStats) bool {
	return a.Count == b.Count && a.DistanceMeters == b.DistanceMeters && a.MovingSeconds == b.MovingSeconds &&
		a.ElapsedSeconds == b.ElapsedSeconds && slices.Equal(a.ByType, b.ByType)
}

func TestStoriesValidation(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	var st story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Trip"}), http.StatusCreated, &st)

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"blank name", "POST", "/v1/stories", map[string]any{"name": "   "}},
		{"long name", "POST", "/v1/stories", map[string]any{"name": strings.Repeat("я", maxStoryNameLen+1)}},
		{"long description", "POST", "/v1/stories", map[string]any{"name": "a", "description": strings.Repeat("x", maxStoryDescriptionLen+1)}},
		{"malformed activity id", "POST", "/v1/stories", map[string]any{"name": "a", "activity_ids": []string{"nope"}}},
		{"blank rename", "PATCH", "/v1/stories/" + st.ID, map[string]any{"name": ""}},
		{"no ids to add", "POST", "/v1/stories/" + st.ID + "/activities", map[string]any{"activity_ids": []string{}}},
		{"no ids to remove", "DELETE", "/v1/stories/" + st.ID + "/activities", map[string]any{}},
		{"not JSON", "POST", "/v1/stories", "{"},
	} {
		if rec := d.do(me, tc.method, tc.path, tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.name, rec.Code)
		}
	}
	// A name of exactly the maximum, counted in characters rather than bytes, is fine.
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": strings.Repeat("я", maxStoryNameLen)}), http.StatusCreated, nil)
	if rec := d.do(account{}, "GET", "/v1/stories", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status %d, want 401", rec.Code)
	}
}

// Another account's Story is a 404 on every route, the same as one that doesn't exist, and
// another account's activity can't be put in a Story.
func TestStoriesOwnership(t *testing.T) {
	d := newDBTest(t)
	owner, other := d.newAccount(false), d.newAccount(false)
	ownerActivity := d.newActivity(owner, testActivity{activityType: "hiking", distanceMeters: 5000, durationSecs: 3600})
	otherActivity := d.newActivity(other, testActivity{activityType: "hiking", distanceMeters: 5000, durationSecs: 3600})
	var theirs, mine story
	d.decode(d.do(owner, "POST", "/v1/stories", map[string]any{"name": "Owner's", "activity_ids": []string{ownerActivity}}), http.StatusCreated, &theirs)
	d.decode(d.do(other, "POST", "/v1/stories", map[string]any{"name": "Other's"}), http.StatusCreated, &mine)

	missing := "/v1/stories/00000000-0000-0000-0000-000000000000"
	for _, path := range []string{"/v1/stories/" + theirs.ID, missing, "/v1/stories/not-a-uuid"} {
		for _, tc := range []struct {
			method, suffix string
			body           any
		}{
			{"GET", "", nil},
			{"PATCH", "", map[string]any{"name": "Taken"}},
			{"DELETE", "", nil},
			{"POST", "/activities", map[string]any{"activity_ids": []string{otherActivity}}},
			{"DELETE", "/activities", map[string]any{"activity_ids": []string{ownerActivity}}},
		} {
			if rec := d.do(other, tc.method, path+tc.suffix, tc.body); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s%s: status %d, want 404", tc.method, path, tc.suffix, rec.Code)
			}
		}
	}
	var got story
	d.decode(d.do(owner, "GET", "/v1/stories/"+theirs.ID, nil), http.StatusOK, &got)
	if got.Name != "Owner's" || !slices.Equal(got.ActivityIDs, []string{ownerActivity}) {
		t.Errorf("owner's Story changed: %+v", got)
	}

	var list storiesResponse
	d.decode(d.do(other, "GET", "/v1/stories", nil), http.StatusOK, &list)
	if len(list.Stories) != 1 || list.Stories[0].ID != mine.ID {
		t.Errorf("other's list %+v, want only their own Story", list.Stories)
	}

	// Someone else's activity: all or nothing, and a 404 like a missing one.
	if rec := d.do(other, "POST", "/v1/stories/"+mine.ID+"/activities", map[string]any{"activity_ids": []string{otherActivity, ownerActivity}}); rec.Code != http.StatusNotFound {
		t.Errorf("adding someone else's activity: status %d, want 404", rec.Code)
	}
	d.decode(d.do(other, "GET", "/v1/stories/"+mine.ID, nil), http.StatusOK, &got)
	if len(got.ActivityIDs) != 0 {
		t.Errorf("a rejected add left members %v", got.ActivityIDs)
	}
	if rec := d.do(other, "POST", "/v1/stories", map[string]any{"name": "Sneaky", "activity_ids": []string{ownerActivity}}); rec.Code != http.StatusNotFound {
		t.Errorf("creating with someone else's activity: status %d, want 404", rec.Code)
	}
	d.decode(d.do(other, "GET", "/v1/stories", nil), http.StatusOK, &list)
	if len(list.Stories) != 1 {
		t.Errorf("a rejected create left a Story behind: %+v", list.Stories)
	}
}

// A demo account reads its Stories but every write is refused, as for every other write.
func TestStoriesDemoReadOnly(t *testing.T) {
	d := newDBTest(t)
	demo := d.newAccount(true)
	activity := d.newActivity(demo, testActivity{activityType: "walking", distanceMeters: 1000, durationSecs: 600})
	var storyID string
	if err := d.pool.QueryRow(context.Background(),
		`INSERT INTO stories (user_id, name) VALUES ($1, 'Seeded') RETURNING id`, demo.id).Scan(&storyID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(),
		`INSERT INTO story_activities (story_id, activity_id) VALUES ($1, $2)`, storyID, activity); err != nil {
		t.Fatal(err)
	}

	var list storiesResponse
	d.decode(d.do(demo, "GET", "/v1/stories", nil), http.StatusOK, &list)
	if len(list.Stories) != 1 || list.Stories[0].Stats.Count != 1 {
		t.Errorf("demo list %+v", list.Stories)
	}
	d.decode(d.do(demo, "GET", "/v1/stories/"+storyID, nil), http.StatusOK, nil)

	ids := map[string]any{"activity_ids": []string{activity}}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/stories", map[string]any{"name": "Mine"}},
		{"PATCH", "/v1/stories/" + storyID, map[string]any{"name": "Renamed"}},
		{"DELETE", "/v1/stories/" + storyID, nil},
		{"POST", "/v1/stories/" + storyID + "/activities", ids},
		{"DELETE", "/v1/stories/" + storyID + "/activities", ids},
	} {
		var body struct{ Error string }
		d.decode(d.do(demo, tc.method, tc.path, tc.body), http.StatusForbidden, &body)
		if body.Error != "demo_read_only" {
			t.Errorf("%s %s: error %q, want demo_read_only", tc.method, tc.path, body.Error)
		}
	}
	var got story
	d.decode(d.do(demo, "GET", "/v1/stories/"+storyID, nil), http.StatusOK, &got)
	if got.Name != "Seeded" || !slices.Equal(got.ActivityIDs, []string{activity}) {
		t.Errorf("demo Story changed: %+v", got)
	}
	d.decode(d.do(demo, "GET", "/v1/stories", nil), http.StatusOK, &list)
	if len(list.Stories) != 1 {
		t.Errorf("demo has %d Stories, want 1", len(list.Stories))
	}
}

// Deleting an activity takes it out of every Story holding it; the Stories stay, even once
// empty.
func TestActivityDeleteLeavesStories(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	first := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 1000, durationSecs: 600})
	second := d.newActivity(me, testActivity{activityType: "cycling", distanceMeters: 9000, durationSecs: 1800,
		startedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)})
	var both, onlyFirst story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Both", "activity_ids": []string{first, second}}), http.StatusCreated, &both)
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Only first", "activity_ids": []string{first}}), http.StatusCreated, &onlyFirst)

	d.decode(d.do(me, "DELETE", "/v1/activities/"+first, nil), http.StatusNoContent, nil)

	var got story
	d.decode(d.do(me, "GET", "/v1/stories/"+both.ID, nil), http.StatusOK, &got)
	if !slices.Equal(got.ActivityIDs, []string{second}) || got.Stats.Count != 1 || got.Stats.DistanceMeters != 9000 {
		t.Errorf("Story with both after deleting one: %+v", got)
	}
	d.decode(d.do(me, "GET", "/v1/stories/"+onlyFirst.ID, nil), http.StatusOK, &got)
	if len(got.ActivityIDs) != 0 || got.Stats.Count != 0 || len(got.Stats.ByType) != 0 || got.Name != "Only first" {
		t.Errorf("Story left empty: %+v", got)
	}
}

// Each row of the activity list names the Stories it's in, newest first — the Activities
// panel's Story badge — and an activity in none has an empty list, not null.
func TestActivityListNamesStories(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	inTwo := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60})
	inNone := d.newActivity(me, testActivity{activityType: "walking", durationSecs: 60,
		startedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)})
	var older, newer story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Older", "activity_ids": []string{inTwo}}), http.StatusCreated, &older)
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Newer", "activity_ids": []string{inTwo}}), http.StatusCreated, &newer)

	var resp activitiesResponse
	d.decode(d.do(me, "GET", "/v1/activities", nil), http.StatusOK, &resp)
	got := map[string][]storyRef{}
	for _, a := range resp.Activities {
		got[a.ID] = a.Stories
	}
	if want := []storyRef{{newer.ID, "Newer"}, {older.ID, "Older"}}; !slices.Equal(got[inTwo], want) {
		t.Errorf("stories of the activity in two: %v, want %v", got[inTwo], want)
	}
	if s, ok := got[inNone]; !ok || s == nil || len(s) != 0 {
		t.Errorf("stories of the activity in none: %v (present %v), want []", s, ok)
	}

	// PATCH answers with the same row.
	var row activityRow
	d.decode(d.do(me, "PATCH", "/v1/activities/"+inTwo, map[string]any{"activity_type": "hiking"}), http.StatusOK, &row)
	if len(row.Stories) != 2 {
		t.Errorf("PATCH row stories: %v", row.Stories)
	}
}
