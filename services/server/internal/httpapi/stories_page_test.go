package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// page requests a server-rendered page as as, in lang; a POST is a form submission from
// origin.
func (d *dbTest) page(as account, method, path, lang, origin string) *httptest.ResponseRecorder {
	d.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if as.session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: as.session})
	}
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)
	return rec
}

func (d *dbTest) storyExists(id string) bool {
	d.t.Helper()
	var ok bool
	if err := d.pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM stories WHERE id = $1)`, id).Scan(&ok); err != nil {
		d.t.Fatal(err)
	}
	return ok
}

// formOrigin is newDBTest's APP_BASE_URL, the Origin a real form POST carries.
const formOrigin = "https://app.example"

func TestStoriesPage(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)

	if rec := d.page(account{}, "GET", "/stories", "", ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/signin" {
		t.Errorf("signed out: %d → %q, want the sign-in page", rec.Code, rec.Header().Get("Location"))
	}

	rec := d.page(me, "GET", "/stories", "", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "No stories yet.") {
		t.Fatalf("empty: status %d, empty state missing", rec.Code)
	}
	if !strings.Contains(body, `href="/stories"`) {
		t.Errorf("the account menu has no Stories item")
	}

	walk := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 3000, movingSeconds: intPtr(1800), durationSecs: 2000})
	drive := d.newActivity(me, testActivity{activityType: "driving", distanceMeters: 40000, movingSeconds: intPtr(2400), durationSecs: 2600,
		startedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)})
	var trip, empty story
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Brecksville <trip>", "description": "The reservation", "activity_ids": []string{walk, drive}}), http.StatusCreated, &trip)
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Nothing yet"}), http.StatusCreated, &empty)

	body = d.page(me, "GET", "/stories", "", "").Body.String()
	for _, want := range []string{
		"Brecksville &lt;trip&gt;", "The reservation",
		"2 activities · 43 km · 1h 10m moving",
		`<th scope="row">Driving</th><td>1 activity · 40 km · 40m moving</td>`,
		`<th scope="row">Walking</th><td>1 activity · 3 km · 30m moving</td>`,
		`href="/?story=` + trip.ID + `"`,
		`action="/stories/` + trip.ID + `/delete"`,
		"No activities in this story.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Index(body, "Nothing yet") > strings.Index(body, "Brecksville") {
		t.Errorf("Stories aren't newest first")
	}
	if ru := d.page(me, "GET", "/stories", "ru", "").Body.String(); !strings.Contains(ru, "Поездка на машине") || !strings.Contains(ru, "Показать на карте") {
		t.Errorf("Russian page lacks its type names or labels")
	}

	// Delete: refused cross-origin; someone else's Story and a malformed id are a 404; then
	// the real thing.
	if rec := d.page(me, "POST", "/stories/"+trip.ID+"/delete", "", "https://evil.example"); rec.Code != http.StatusForbidden || !d.storyExists(trip.ID) {
		t.Errorf("cross-origin delete: status %d, story exists %v", rec.Code, d.storyExists(trip.ID))
	}
	other := d.newAccount(false)
	if rec := d.page(other, "POST", "/stories/"+trip.ID+"/delete", "", formOrigin); rec.Code != http.StatusNotFound || !d.storyExists(trip.ID) {
		t.Errorf("another account's delete: status %d, story exists %v", rec.Code, d.storyExists(trip.ID))
	}
	if rec := d.page(me, "POST", "/stories/not-a-uuid/delete", "", formOrigin); rec.Code != http.StatusNotFound {
		t.Errorf("malformed id: status %d, want 404", rec.Code)
	}
	rec = d.page(me, "POST", "/stories/"+trip.ID+"/delete", "", formOrigin)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/stories?deleted" || d.storyExists(trip.ID) {
		t.Fatalf("delete: %d → %q, story exists %v", rec.Code, rec.Header().Get("Location"), d.storyExists(trip.ID))
	}
	body = d.page(me, "GET", "/stories?deleted", "", "").Body.String()
	if !strings.Contains(body, "Story deleted.") || strings.Contains(body, "Brecksville") {
		t.Errorf("after delete: notice or leftover Story wrong")
	}
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM activities WHERE user_id = $1`, me.id).Scan(&n); err != nil || n != 2 {
		t.Errorf("activities after deleting the Story: %d (%v), want 2", n, err)
	}
}

func TestStoriesPageImperial(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET country = 'US' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	walk := d.newActivity(me, testActivity{activityType: "walking", distanceMeters: 16093.44, movingSeconds: intPtr(7200), durationSecs: 7200})
	d.decode(d.do(me, "POST", "/v1/stories", map[string]any{"name": "Ten miles", "activity_ids": []string{walk}}), http.StatusCreated, nil)
	if body := d.page(me, "GET", "/stories", "", "").Body.String(); !strings.Contains(body, "1 activity · 10 mi · 2h 0m moving") {
		t.Errorf("an imperial account's stats aren't in miles")
	}
}

// A demo session sees the Demo Customer's Stories with Delete disabled, and a Delete posted
// anyway is refused.
func TestStoriesPageDemo(t *testing.T) {
	d := newDBTest(t)
	demo := d.newAccount(true)
	var id string
	if err := d.pool.QueryRow(context.Background(), `INSERT INTO stories (user_id, name) VALUES ($1, 'Seeded') RETURNING id`, demo.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := d.page(demo, "GET", "/stories", "", "").Body.String()
	if !strings.Contains(body, "Seeded") || strings.Contains(body, `action="/stories/`+id+`/delete"`) || !strings.Contains(body, "disabled>Delete</button>") {
		t.Errorf("demo page: want the Story with Delete disabled and no delete form")
	}
	rec := d.page(demo, "POST", "/stories/"+id+"/delete", "", formOrigin)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Demo accounts can") || !d.storyExists(id) {
		t.Errorf("demo delete: status %d, story exists %v", rec.Code, d.storyExists(id))
	}
}
