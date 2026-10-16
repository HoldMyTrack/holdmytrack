package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// northGPX is a walk due north along lon, eleven fixes a minute and about 111 m apart, from
// lat 50.000 to 50.010.
func northGPX(lon float64) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><gpx><trk><trkseg>`)
	for i := 0; i <= 10; i++ {
		fmt.Fprintf(&b, `<trkpt lat="%.4f" lon="%.4f"><time>2026-05-01T10:%02d:00Z</time></trkpt>`, 50+float64(i)*0.001, lon, i)
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	return []byte(b.String())
}

// ingestNorth uploads northGPX(lon) as name and ingests it, returning the activity's id.
func (d *dbTest) ingestNorth(as account, name string, lon float64) string {
	d.t.Helper()
	if resp := d.uploadFile(as, name, northGPX(lon)); resp.Status != "enqueued" {
		d.t.Fatalf("upload %s: %q", name, resp.Status)
	}
	res, err := ingest.Process(context.Background(), d.pool, d.srv.store, d.latestIngestJob(as))
	if err != nil || !res.Persisted {
		d.t.Fatalf("ingest %s: %+v, %v", name, res, err)
	}
	return res.ActivityID
}

// reprivacyJobs returns the account's pending reprivacy jobs, oldest first.
func (d *dbTest) reprivacyJobs(as account) []ingest.ReprivacyJob {
	d.t.Helper()
	rows, err := d.pool.Query(context.Background(),
		`SELECT payload FROM jobs WHERE user_id = $1 AND kind = 'reprivacy' AND state = 'pending' ORDER BY id`, as.id)
	if err != nil {
		d.t.Fatal(err)
	}
	defer rows.Close()
	var jobs []ingest.ReprivacyJob
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			d.t.Fatal(err)
		}
		var j ingest.ReprivacyJob
		if err := json.Unmarshal(payload, &j); err != nil {
			d.t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	return jobs
}

// runReprivacyJob runs the account's one pending reprivacy job as the worker would, returning
// the activities it reprocessed.
func (d *dbTest) runReprivacyJob(as account) []string {
	d.t.Helper()
	ctx := context.Background()
	var id int64
	var payload []byte
	if err := d.pool.QueryRow(ctx,
		`SELECT id, payload FROM jobs WHERE user_id = $1 AND kind = 'reprivacy' AND state = 'pending'`, as.id).Scan(&id, &payload); err != nil {
		d.t.Fatalf("one pending reprivacy job: %v", err)
	}
	var j ingest.ReprivacyJob
	if err := json.Unmarshal(payload, &j); err != nil {
		d.t.Fatal(err)
	}
	if err := ingest.ProcessReprivacy(ctx, d.pool, d.srv.store, id, j); err != nil {
		d.t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE jobs SET state = 'done' WHERE id = $1`, id); err != nil {
		d.t.Fatal(err)
	}
	return j.ActivityIDs
}

func (d *dbTest) activity(as account, id string) activityRow {
	d.t.Helper()
	for _, a := range d.listActivities(as) {
		if a.ID == id {
			return a
		}
	}
	d.t.Fatalf("activity %s not listed", id)
	return activityRow{}
}

// nearestM is how close the activity's stored trajectory comes to (lon, lat), in metres.
func (d *dbTest) nearestM(id string, lon, lat float64) float64 {
	d.t.Helper()
	var m float64
	if err := d.pool.QueryRow(context.Background(), `
		SELECT ST_Distance(trajectory::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography)
		FROM activities WHERE id = $1`, id, lon, lat).Scan(&m); err != nil {
		d.t.Fatal(err)
	}
	return m
}

// Adding, moving, resizing and deleting a Private location reprocesses the activities it
// touches, from their original uploads, against the account's locations as they are now
// (FR-8.1, IMPLEMENTATION.md §7): a track's hidden end follows the location and comes back
// once it leaves, and an activity it covers entirely is hidden and then restored.
func TestPrivateLocationReprocessesActivities(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	me := d.newAccount(false)
	walk := d.ingestNorth(me, "walk.gpx", 10.0)
	other := d.ingestNorth(me, "other.gpx", 11.0) // about 70 km east of walk

	distance := func(id string) float64 {
		t.Helper()
		a := d.activity(me, id)
		if a.DistanceMeters == nil {
			return 0
		}
		return *a.DistanceMeters
	}
	full := d.activity(me, walk)
	fullDistance := distance(walk)
	if full.BBox == nil || fullDistance < 1000 || math.Abs(distance(other)-fullDistance) > 1 {
		t.Fatalf("before any location: walk %+v, other %.0f m", full, distance(other))
	}
	const radiusM = 250
	whole := func(label, id string) {
		t.Helper()
		if got := distance(id); math.Abs(got-fullDistance) > 1 {
			t.Errorf("%s: distance %.0f m, want the whole %.0f", label, got, fullDistance)
		}
	}
	clippedBy := func(label, id string, lon, lat float64) {
		t.Helper()
		// The end is cut up to another radius past the edge (ingest.ClipEnds).
		if lost := fullDistance - distance(id); lost < radiusM-50 || lost > 2*radiusM+50 {
			t.Errorf("%s: %.0f m shorter than the whole %.0f, want %d–%d m", label, lost, fullDistance, radiusM, 2*radiusM)
		}
		if near := d.nearestM(id, lon, lat); near < radiusM-1 {
			t.Errorf("%s: track comes within %.0f m of the location, want at least %d", label, near, radiusM)
		}
	}
	move := func(id string, body map[string]any) {
		t.Helper()
		d.decode(d.do(me, "PATCH", "/v1/private-locations/"+id, body), http.StatusOK, nil)
	}

	// Add one at walk's start: only walk is queued and marked Pending, and once the job runs its
	// start is gone up to past the location's edge.
	var home privateLocation
	d.decode(d.do(me, "POST", "/v1/private-locations", map[string]any{"name": "Home", "lon": 10.0, "lat": 50.0, "radius_m": radiusM}), http.StatusCreated, &home)
	if jobs := d.reprivacyJobs(me); len(jobs) != 1 || !slices.Equal(jobs[0].ActivityIDs, []string{walk}) {
		t.Fatalf("after create, jobs %+v, want one for walk only", jobs)
	}
	if !d.activity(me, walk).Pending || d.activity(me, other).Pending {
		t.Error("after create, want only walk Pending")
	}
	d.runReprivacyJob(me)
	if a := d.activity(me, walk); a.Pending || a.Private {
		t.Errorf("after create's job: %+v, want shown and no longer Pending", a)
	}
	clippedBy("created", walk, 10.0, 50.0)
	whole("created, other", other)

	// Move it to other's end: the old circle's activities are reprocessed as well as the new
	// one's, so walk gets its start back.
	move(home.ID, map[string]any{"name": "Home", "lon": 11.0, "lat": 50.01, "radius_m": radiusM})
	d.runReprivacyJob(me)
	whole("moved away", walk)
	clippedBy("moved to other", other, 11.0, 50.01)

	// A rename changes nothing on the map, so nothing is queued.
	move(home.ID, map[string]any{"name": "Flat", "lon": 11.0, "lat": 50.01, "radius_m": radiusM})
	if jobs := d.reprivacyJobs(me); len(jobs) != 0 {
		t.Errorf("after rename, jobs %+v, want none", jobs)
	}

	// Move and grow it over the whole of walk: walk is kept but hidden, with no track at all.
	move(home.ID, map[string]any{"name": "Flat", "lon": 10.0, "lat": 50.005, "radius_m": 2000})
	d.runReprivacyJob(me)
	if a := d.activity(me, walk); !a.Private || a.BBox != nil || a.Pending {
		t.Errorf("covered: %+v, want kept, Private, with no track", a)
	}
	whole("moved away, other", other)

	// Delete it: hidden walk has no track to find it by, and still comes back whole.
	if rec := d.do(me, "DELETE", "/v1/private-locations/"+home.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	d.runReprivacyJob(me)
	restored := d.activity(me, walk)
	if restored.Private || restored.Pending || !slices.Equal(restored.BBox, full.BBox) {
		t.Errorf("after delete: %+v, want walk back as it was (bbox %v)", restored, full.BBox)
	}
	whole("deleted", walk)
}
