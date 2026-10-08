package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storycopy"
)

// recorded ingests a walk for owner the way an upload does, from a GPX file in the store: 61
// points a minute apart heading east along latitude 50 from lon, so a Private location can clip
// either end.
func (d *dbTest) recorded(s3 *memS3, owner account, name string, start time.Time, lon float64) string {
	d.t.Helper()
	ctx := context.Background()
	points := make([]parse.Point, 61)
	for i := range points {
		points[i] = parse.Point{Lat: 50, Lon: lon + float64(i)*0.001, Time: start.Add(time.Duration(i) * time.Minute)}
	}
	var gpx bytes.Buffer
	if err := export.WriteGPX(&gpx, "walking", points); err != nil {
		d.t.Fatal(err)
	}
	key := fmt.Sprintf("raw/%s/%s.gpx", owner.id, name)
	s3.Put(key, gpx.Bytes())
	res, err := ingest.Process(ctx, d.pool, d.srv.store, ingest.Job{
		UserID: owner.id, Source: "upload", SourceDetail: name + ".gpx", ExternalID: name, RawPayloadKey: key,
	})
	if err != nil {
		d.t.Fatalf("ingest %s: %v", name, err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET name = $2, description = 'Day out' WHERE id = $1`, res.ActivityID, name); err != nil {
		d.t.Fatal(err)
	}
	return res.ActivityID
}

func (d *dbTest) privateLocation(owner account, lon, lat float64, radiusM int) {
	d.t.Helper()
	if _, err := d.pool.Exec(context.Background(), `
		INSERT INTO privacy_zones (user_id, center, radius_m, name) VALUES ($1, ST_MakePoint($2, $3)::geography, $4, 'Home')`,
		owner.id, lon, lat, radiusM); err != nil {
		d.t.Fatal(err)
	}
}

// sendAndAccept sends the Story from from to to, accepts it as to and runs the copy job.
func (d *dbTest) sendAndAccept(from, to account, storyID string) {
	d.t.Helper()
	d.decode(d.do(from, "POST", "/v1/stories/"+storyID+"/send", map[string]any{"email": d.address(to)}), http.StatusNoContent, nil)
	sends := d.inbox(to)
	if len(sends) != 1 {
		d.t.Fatalf("inbox %+v, want the one copy", sends)
	}
	d.decode(d.do(to, "POST", "/v1/story-sends/"+sends[0].ID+"/accept", nil), http.StatusAccepted, nil)
	if err := storycopy.Process(context.Background(), d.pool, d.srv.store, storycopy.Job{StoryID: storyID, RecipientID: to.id}); err != nil {
		d.t.Fatalf("copy: %v", err)
	}
}

// storiesOf lists the account's Stories, newest first.
func (d *dbTest) storiesOf(as account) []story {
	d.t.Helper()
	var res storiesResponse
	d.decode(d.do(as, "GET", "/v1/stories", nil), http.StatusOK, &res)
	return res.Stories
}

// copied is one of the recipient's activities as the copy made it.
type copied struct {
	id, name, origin string
	startLon, endLon float64
}

func (d *dbTest) copiedActivity(id string) copied {
	d.t.Helper()
	var c copied
	if err := d.pool.QueryRow(context.Background(), `
		SELECT id, COALESCE(name, ''), COALESCE(origin_id::text, ''), ST_X(ST_StartPoint(trajectory)), ST_X(ST_EndPoint(trajectory))
		FROM activities WHERE id = $1`, id).Scan(&c.id, &c.name, &c.origin, &c.startLon, &c.endLon); err != nil {
		d.t.Fatal(err)
	}
	return c
}

func TestStoryCopy(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	ctx := context.Background()
	dad, kid := d.newAccount(false), d.newAccount(false)
	day := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	// Dad's home is at the walk's start, the kid's at its end: the copy is clipped by both.
	d.privateLocation(dad, 10, 50, 150)
	d.privateLocation(kid, 10.06, 50, 150)
	walk := d.recorded(s3, dad, "Ridge", day, 10)
	var p photoJSON
	d.decode(d.uploadPhoto(dad, testJPEG(t, 40, 30), testJPEG(t, 8, 6), map[string]string{"activity_id": walk, "route_at": day.Add(30 * time.Minute).Format(time.RFC3339)}), http.StatusCreated, &p)
	trip := d.newStory(dad, "Alps", walk)

	d.sendAndAccept(dad, kid, trip)
	stories := d.storiesOf(kid)
	if len(stories) != 1 || stories[0].Name != "Alps" || len(stories[0].ActivityIDs) != 1 {
		t.Fatalf("kid's stories %+v, want Alps with one activity", stories)
	}
	if from := stories[0].From; from == nil || *from != d.address(dad) {
		t.Errorf("from %v, want Dad's address (he has no name set)", from)
	}
	if own := d.storiesOf(dad); own[0].From != nil {
		t.Errorf("Dad's own Story says it's from %q", *own[0].From)
	}
	got := d.copiedActivity(stories[0].ActivityIDs[0])
	if got.name != "Ridge" || got.origin != walk {
		t.Errorf("copy %+v, want Ridge from %s", got, walk)
	}
	if got.startLon < 10.002 || got.endLon > 10.058 {
		t.Errorf("copy runs %v → %v, want both homes clipped off", got.startLon, got.endLon)
	}
	var list photosResponse
	d.decode(d.do(kid, "GET", "/v1/photos?activity="+got.id, nil), http.StatusOK, &list)
	if len(list.Photos) != 1 {
		t.Fatalf("copy's photos %+v, want the one", list.Photos)
	}
	var keys int
	d.pool.QueryRow(ctx, `SELECT count(DISTINCT image_key) FROM activity_photos WHERE id IN ($1, $2)`, p.ID, list.Photos[0].ID).Scan(&keys)
	if keys != 1 {
		t.Errorf("copied photo has its own files, want the original's")
	}

	// Sent again with a second walk: only the new one arrives, into the same Story.
	second := d.recorded(s3, dad, "Lake", day.Add(24*time.Hour), 11)
	d.decode(d.do(dad, "POST", "/v1/stories/"+trip+"/activities", map[string]any{"activity_ids": []string{second}}), http.StatusOK, nil)
	d.sendAndAccept(dad, kid, trip)
	stories = d.storiesOf(kid)
	if len(stories) != 1 || len(stories[0].ActivityIDs) != 2 {
		t.Fatalf("after the second send: %+v, want one Story with two activities", stories)
	}

	// A copy the kid deleted doesn't come back.
	d.decode(d.do(kid, "DELETE", "/v1/activities/"+got.id, nil), http.StatusNoContent, nil)
	d.sendAndAccept(dad, kid, trip)
	if stories = d.storiesOf(kid); len(stories[0].ActivityIDs) != 1 {
		t.Errorf("after deleting a copy and a third send: %d activities, want 1", len(stories[0].ActivityIDs))
	}
	// Its photo files stayed with Dad's row.
	if !s3.Has(photoKey(dad.id, p.ID)) {
		t.Errorf("deleting the copy removed the original's photo")
	}

	// Sent back to Dad: everything in it is his own, so no Story is made.
	d.sendAndAccept(kid, dad, stories[0].ID)
	if mine := d.storiesOf(dad); len(mine) != 1 {
		t.Errorf("Dad's stories after the kid sent theirs back: %d, want 1", len(mine))
	}

	// The kid deletes their Story; the next send makes a new one with only what's new.
	d.decode(d.do(kid, "DELETE", "/v1/stories/"+stories[0].ID, nil), http.StatusNoContent, nil)
	third := d.recorded(s3, dad, "Summit", day.Add(48*time.Hour), 12)
	d.decode(d.do(dad, "POST", "/v1/stories/"+trip+"/activities", map[string]any{"activity_ids": []string{third}}), http.StatusOK, nil)
	d.sendAndAccept(dad, kid, trip)
	stories = d.storiesOf(kid)
	if len(stories) != 1 || len(stories[0].ActivityIDs) != 1 || d.copiedActivity(stories[0].ActivityIDs[0]).origin != third {
		t.Fatalf("after deleting the Story: %+v, want a new one with only Summit", stories)
	}

	// Dad's edits and deletes don't reach the kid's copies.
	d.decode(d.do(dad, "DELETE", "/v1/activities/"+third, nil), http.StatusNoContent, nil)
	if stories = d.storiesOf(kid); len(stories[0].ActivityIDs) != 1 {
		t.Errorf("Dad's delete reached the kid's copy")
	}
}

func TestStoryCopyWaitsForAPendingEdit(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	ctx := context.Background()
	dad, kid := d.newAccount(false), d.newAccount(false)
	walk := d.recorded(s3, dad, "Ridge", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC), 10)
	trip := d.newStory(dad, "Alps", walk)
	if _, err := d.pool.Exec(ctx, `UPDATE activities SET edit_pending = true WHERE id = $1`, walk); err != nil {
		t.Fatal(err)
	}
	d.sendAndAccept(dad, kid, trip)
	if stories := d.storiesOf(kid); len(stories) != 0 {
		t.Errorf("copied mid-edit: %+v", stories)
	}
	var waiting int
	d.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'story_copy' AND user_id = $1 AND run_after > NOW()`, kid.id).Scan(&waiting)
	if waiting != 1 {
		t.Errorf("%d retries queued, want 1", waiting)
	}
}

func TestStoryCopyOfAnEmptyStory(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	dad, kid := d.newAccount(false), d.newAccount(false)
	trip := d.newStory(dad, "Someday")
	d.sendAndAccept(dad, kid, trip)
	if stories := d.storiesOf(kid); len(stories) != 1 || stories[0].Name != "Someday" {
		t.Errorf("kid's stories %+v, want the empty Someday", stories)
	}
}