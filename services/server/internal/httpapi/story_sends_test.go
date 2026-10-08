package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// address is the account's email address, as a sender would type it.
func (d *dbTest) address(a account) string {
	d.t.Helper()
	email, _ := d.email(a)
	return email
}

func (d *dbTest) newStory(owner account, name string, activityIDs ...string) string {
	d.t.Helper()
	var st story
	d.decode(d.do(owner, "POST", "/v1/stories", map[string]any{"name": name, "activity_ids": activityIDs}), http.StatusCreated, &st)
	return st.ID
}

func (d *dbTest) inbox(as account) []storySendJSON {
	d.t.Helper()
	var res storySendsResponse
	d.decode(d.do(as, "GET", "/v1/story-sends", nil), http.StatusOK, &res)
	return res.Sends
}

func TestSendAStoryCopy(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me, you := d.newAccount(false), d.newAccount(false)
	if _, err := d.pool.Exec(ctx, `UPDATE users SET display_name = 'Dad' WHERE id = $1`, me.id); err != nil {
		t.Fatal(err)
	}
	walk := d.newActivity(me, testActivity{activityType: "walking", at: &[2]float64{10, 50}})
	trip := d.newStory(me, "Alps", walk)

	// Sent twice: one item, with the address typed in any case.
	for range 2 {
		d.decode(d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": "  " + strings.ToUpper(d.address(you)) + " "}), http.StatusNoContent, nil)
	}
	got := d.inbox(you)
	if len(got) != 1 || got[0].StoryName != "Alps" || got[0].From != "Dad" || got[0].ActivityCount != 1 {
		t.Fatalf("inbox %+v, want one copy of Alps from Dad", got)
	}
	mails := d.srv.mailer.(*sentMail)
	if len(mails.to) != 2 || mails.to[0] != d.address(you) || mails.subjects[0] != "Dad sent you a copy of “Alps”" ||
		!strings.Contains(mails.bodies[0], "(1 activity)") || !strings.Contains(mails.bodies[0], "https://app.example/?tab=stories") {
		t.Errorf("emails %q / %q / %q, want one per send to the recipient", mails.to, mails.subjects, mails.bodies)
	}
	if mine := d.inbox(me); len(mine) != 0 {
		t.Errorf("the sender's own inbox: %+v", mine)
	}

	// Declined: gone, nothing queued.
	d.decode(d.do(you, "DELETE", "/v1/story-sends/"+got[0].ID, nil), http.StatusNoContent, nil)
	if left := d.inbox(you); len(left) != 0 {
		t.Errorf("after decline: %+v", left)
	}

	// Accepted: gone, and a copy job queued for the recipient.
	d.decode(d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": d.address(you)}), http.StatusNoContent, nil)
	got = d.inbox(you)
	if len(got) != 1 {
		t.Fatalf("inbox after sending again: %+v", got)
	}
	if rec := d.do(me, "POST", "/v1/story-sends/"+got[0].ID+"/accept", nil); rec.Code != http.StatusNotFound {
		t.Errorf("accepted by the sender: %d, want 404", rec.Code)
	}
	d.decode(d.do(you, "POST", "/v1/story-sends/"+got[0].ID+"/accept", nil), http.StatusAccepted, nil)
	var jobs int
	d.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'story_copy' AND user_id = $1 AND payload->>'story_id' = $2`, you.id, trip).Scan(&jobs)
	if jobs != 1 || len(d.inbox(you)) != 0 {
		t.Errorf("after accept: %d copy jobs, inbox %+v", jobs, d.inbox(you))
	}
	var res storySendsResponse
	d.decode(d.do(you, "GET", "/v1/story-sends", nil), http.StatusOK, &res)
	if len(res.Copying) != 1 || res.Copying[0].StoryName != "Alps" || res.Copying[0].From != "Dad" {
		t.Errorf("still copying: %+v, want Alps from Dad", res.Copying)
	}
	if rec := d.do(you, "POST", "/v1/story-sends/"+got[0].ID+"/accept", nil); rec.Code != http.StatusNotFound {
		t.Errorf("accepted twice: %d, want 404", rec.Code)
	}

	// Deleting the Story withdraws a copy still waiting.
	d.decode(d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": d.address(you)}), http.StatusNoContent, nil)
	d.decode(d.do(me, "DELETE", "/v1/stories/"+trip, nil), http.StatusNoContent, nil)
	if left := d.inbox(you); len(left) != 0 {
		t.Errorf("after the Story's delete: %+v", left)
	}
}

func TestSendAStoryCopyRevealsNoAccount(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)
	trip := d.newStory(me, "Alps")
	unverified, demo, gone := d.newAccount(false), d.newAccount(true), d.newAccount(false)
	if _, err := d.pool.Exec(ctx, `UPDATE users SET email_verified = false WHERE id = $1`, unverified.id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, gone.id); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"nobody-" + me.id + "@holdmytrack.invalid", d.address(unverified), d.address(demo), d.address(gone), d.address(me)} {
		d.decode(d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": email}), http.StatusNoContent, nil)
	}
	var sends int
	d.pool.QueryRow(ctx, `SELECT count(*) FROM story_sends WHERE story_id = $1`, trip).Scan(&sends)
	if sends != 0 {
		t.Errorf("%d copies stored for accounts that can't receive one", sends)
	}
	if mails := d.srv.mailer.(*sentMail); len(mails.to) != 0 {
		t.Errorf("emailed %q, who can't receive a copy", mails.to)
	}

	other := d.newAccount(false)
	if rec := d.do(other, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": d.address(other)}); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's Story: %d, want 404", rec.Code)
	}
	if rec := d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": "Dad <dad@example.com>"}); rec.Code != http.StatusBadRequest {
		t.Errorf("not a bare address: %d, want 400", rec.Code)
	}
	if rec := d.do(demo, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": d.address(me)}); rec.Code != http.StatusForbidden {
		t.Errorf("a demo sending: %d, want 403", rec.Code)
	}
	if got := d.inbox(demo); len(got) != 0 {
		t.Errorf("a demo's inbox: %+v", got)
	}
}

func TestSendAStoryCopyIsRateLimited(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	trip := d.newStory(me, "Alps")
	email := "nobody-" + me.id + "@holdmytrack.invalid"
	for i := range 30 {
		if rec := d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": email}); rec.Code != http.StatusNoContent {
			t.Fatalf("send %d: %d", i+1, rec.Code)
		}
	}
	if rec := d.do(me, "POST", "/v1/stories/"+trip+"/send", map[string]any{"email": email}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("past the limit: %d, want 429", rec.Code)
	}
}
