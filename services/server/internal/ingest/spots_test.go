package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/spots"
)

// Spots matching (§4.25) against a real PostGIS, like httpapi's database tests: skipped without
// TEST_DATABASE_URL. Every case gets its own account, its own spots at a random spot on the
// globe, and removes both again.

// metersLat is how many degrees of latitude a metre is; the fixtures sit near the equator,
// where a degree of longitude is the same length.
const metersLat = 1.0 / 111_320

type spotsTest struct {
	t          *testing.T
	pool       *pgxpool.Pool
	userID     string
	lat, lon   float64 // the fixture's origin
	t0         time.Time
	spotIDs    map[string]int64
	nextOSMID  int64
	activityNo int
}

func newSpotsTest(t *testing.T) *spotsTest {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	st := &spotsTest{
		t: t, pool: pool,
		// Somewhere near the equator nothing else in the test database is: a random degree square.
		lat:       float64(rand.IntN(20)) - 10 + 0.5,
		lon:       float64(rand.IntN(340)) - 170 + 0.5,
		t0:        time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		spotIDs:   map[string]int64{},
		nextOSMID: time.Now().UnixNano(),
	}
	email := fmt.Sprintf("spots-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, email_verified) VALUES ($1, true) RETURNING id`, email).Scan(&st.userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, id := range st.spotIDs {
			if _, err := pool.Exec(ctx, `DELETE FROM spots WHERE id = $1`, id); err != nil {
				t.Errorf("delete spot: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, st.userID); err != nil {
			t.Errorf("delete account: %v", err)
		}
		// Import queued a backfill no worker will run against the test database.
		if _, err := pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'match_spots' AND state = 'pending'`); err != nil {
			t.Errorf("delete backfill job: %v", err)
		}
	})
	return st
}

// at is the point dx metres east and dy metres north of the fixture's origin.
func (st *spotsTest) at(dx, dy float64) (lat, lon float64) {
	return st.lat + dy*metersLat, st.lon + dx*metersLat
}

// importSpots loads GeoJSON features through the real importer, naming each spot's id by key.
func (st *spotsTest) importSpots(features map[string]string) {
	st.t.Helper()
	var lines []string
	osmIDs := map[string]int64{}
	for key, geometry := range features {
		st.nextOSMID++
		osmIDs[key] = st.nextOSMID
		lines = append(lines, fmt.Sprintf(`{"type":"Feature","id":"w%d","geometry":%s,"properties":{"leisure":"playground","name":%q}}`, st.nextOSMID, geometry, key))
	}
	if _, err := spots.Import(context.Background(), st.pool, discardLog(), strings.NewReader(strings.Join(lines, "\n"))); err != nil {
		st.t.Fatalf("import spots: %v", err)
	}
	for key, osmID := range osmIDs {
		var id int64
		if err := st.pool.QueryRow(context.Background(), `SELECT id FROM spots WHERE osm_type = 'way' AND osm_id = $1`, osmID).Scan(&id); err != nil {
			st.t.Fatalf("spot %s not imported: %v", key, err)
		}
		st.spotIDs[key] = id
	}
}

// pointGeoJSON is a place mapped as a single node.
func (st *spotsTest) pointGeoJSON(dx, dy float64) string {
	lat, lon := st.at(dx, dy)
	return fmt.Sprintf(`{"type":"Point","coordinates":[%f,%f]}`, lon, lat)
}

// squareGeoJSON is an outline from (x0, y0) to (x1, y1), in metres from the origin.
func (st *spotsTest) squareGeoJSON(x0, y0, x1, y1 float64) string {
	aLat, aLon := st.at(x0, y0)
	bLat, bLon := st.at(x1, y1)
	return fmt.Sprintf(`{"type":"Polygon","coordinates":[[[%f,%f],[%f,%f],[%f,%f],[%f,%f],[%f,%f]]]}`,
		aLon, aLat, bLon, aLat, bLon, bLat, aLon, bLat, aLon, aLat)
}

// leg is a stretch of track: from (x0, y0) to (x1, y1) in metres, over seconds, a point every
// step seconds.
type leg struct {
	x0, y0, x1, y1 float64
	seconds, step  int
}

// track strings legs together, each starting where time left off.
func (st *spotsTest) track(legs ...leg) []parse.Point {
	var points []parse.Point
	t := st.t0
	for _, l := range legs {
		for s := 0; s <= l.seconds; s += l.step {
			f := float64(s) / float64(l.seconds)
			lat, lon := st.at(l.x0+(l.x1-l.x0)*f, l.y0+(l.y1-l.y0)*f)
			points = append(points, pt(lat, lon, t.Add(time.Duration(s)*time.Second)))
		}
		t = t.Add(time.Duration(l.seconds+l.step) * time.Second)
	}
	return points
}

// activity persists an activity whose trajectory is points, the way ingest would before
// matching, and matches it.
func (st *spotsTest) activity(points []parse.Point) string {
	st.t.Helper()
	ctx := context.Background()
	st.activityNo++
	lons, lats, ts := make([]float64, len(points)), make([]float64, len(points)), make([]float64, len(points))
	for i, p := range points {
		lons[i], lats[i], ts[i] = p.Lon, p.Lat, float64(p.Time.Unix())
	}
	var id string
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, external_id, activity_type, started_at, trajectory)
		VALUES ($1, 'upload', $2, 'walking', $3, `+trajectorySQL("$4", "$5", "$6")+`) RETURNING id
	`, st.userID, fmt.Sprintf("spots-%d", st.activityNo), points[0].Time, lons, lats, ts).Scan(&id); err != nil {
		st.t.Fatalf("create activity: %v", err)
	}
	st.match(id, points)
	return id
}

func (st *spotsTest) match(activityID string, points []parse.Point) {
	st.t.Helper()
	if err := matchSpots(context.Background(), st.pool, st.userID, activityID, points); err != nil {
		st.t.Fatalf("match spots: %v", err)
	}
}

// visits is activityID's visits, spot key → dwell seconds.
func (st *spotsTest) visits(activityID string) map[string]int {
	st.t.Helper()
	rows, err := st.pool.Query(context.Background(), `SELECT spot_id, dwell_seconds FROM spot_visits WHERE activity_id = $1`, activityID)
	if err != nil {
		st.t.Fatalf("read visits: %v", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var spotID int64
		var dwell int
		if err := rows.Scan(&spotID, &dwell); err != nil {
			st.t.Fatalf("read visits: %v", err)
		}
		for key, id := range st.spotIDs {
			if id == spotID {
				out[key] = dwell
			}
		}
	}
	return out
}

func TestSpotVisits(t *testing.T) {
	st := newSpotsTest(t)
	// A park outlined 200 m east, 100 m on a side, and a statue mapped as a point 400 m east.
	st.importSpots(map[string]string{
		"park":   st.squareGeoJSON(200, -50, 300, 50),
		"statue": st.pointGeoJSON(400, 0),
	})

	// Each stay's points all lie inside its spot, and the legs before and after it end and start
	// outside, so a stay's dwell is exactly its own length: a step across the edge never counts.
	in, out := leg{0, 0, 190, 0, 120, 10}, leg{310, 0, 600, 0, 120, 10}
	cases := []struct {
		name string
		legs []leg
		want map[string]int // spot → dwell; absent means not visited
	}{
		{
			name: "a stay of exactly five minutes",
			legs: []leg{in, {240, 0, 260, 20, 300, 10}, out},
			want: map[string]int{"park": 300},
		},
		{
			name: "a stay just over five minutes",
			legs: []leg{in, {240, 0, 260, 20, 310, 10}, out},
			want: map[string]int{"park": 310},
		},
		{
			name: "a stay just under five minutes",
			legs: []leg{in, {240, 0, 260, 20, 290, 10}, out},
		},
		{
			// 30 m/s straight through both: a few seconds inside each.
			name: "a drive past",
			legs: []leg{{0, 0, 600, 0, 20, 1}},
		},
		{
			// Standing 40 m from a node is inside its 50 m circle.
			name: "a stay inside a point-mapped place's circle",
			legs: []leg{{0, 0, 340, 40, 200, 10}, {400, 40, 405, 40, 400, 10}},
			want: map[string]int{"statue": 400},
		},
		{
			name: "a stay just outside a point-mapped place's circle",
			legs: []leg{{0, 0, 340, 60, 200, 10}, {400, 60, 405, 60, 400, 10}},
		},
		{
			// Two four-minute stays add up; the walk outside between them doesn't count.
			name: "leaving and coming back",
			legs: []leg{in, {240, 0, 250, 0, 240, 10}, {150, 0, 150, 10, 120, 10}, {240, 10, 250, 10, 240, 10}, out},
			want: map[string]int{"park": 480},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := st.activity(st.track(c.legs...))
			got := st.visits(id)
			if fmt.Sprint(got) != fmt.Sprint(orEmpty(c.want)) {
				t.Errorf("visits = %v, want %v", got, orEmpty(c.want))
			}
		})
	}
}

// A spot inside a Private location is never visited — even by a track that only passes through
// the location, which the end clip leaves in place.
func TestSpotVisitsSkipPrivateLocations(t *testing.T) {
	st := newSpotsTest(t)
	st.importSpots(map[string]string{"park": st.squareGeoJSON(200, -50, 300, 50)})
	points := st.track(leg{0, 0, 190, 0, 120, 10}, leg{240, 0, 250, 20, 600, 10}, leg{310, 0, 600, 0, 120, 10})

	id := st.activity(points)
	if got := st.visits(id); got["park"] == 0 {
		t.Fatalf("without a Private location: visits = %v, want the park", got)
	}

	lat, lon := st.at(250, 0)
	if _, err := st.pool.Exec(context.Background(), `
		INSERT INTO privacy_zones (user_id, center, radius_m) VALUES ($1, ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography, 150)
	`, st.userID, lat, lon); err != nil {
		t.Fatalf("create private location: %v", err)
	}
	// A reprocess replaces the activity's visits.
	st.match(id, points)
	if got := st.visits(id); len(got) != 0 {
		t.Errorf("inside a Private location: visits = %v, want none", got)
	}
}

// Deleting the activity takes its visits with it.
func TestSpotVisitsCascadeWithActivity(t *testing.T) {
	st := newSpotsTest(t)
	st.importSpots(map[string]string{"park": st.squareGeoJSON(200, -50, 300, 50)})
	id := st.activity(st.track(leg{0, 0, 190, 0, 120, 10}, leg{240, 0, 250, 20, 600, 10}))
	if len(st.visits(id)) != 1 {
		t.Fatalf("visits = %v, want the park", st.visits(id))
	}
	if _, err := st.pool.Exec(context.Background(), `DELETE FROM activities WHERE id = $1`, id); err != nil {
		t.Fatalf("delete activity: %v", err)
	}
	if got := st.visits(id); len(got) != 0 {
		t.Errorf("after delete: visits = %v, want none", got)
	}
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func orEmpty(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}
