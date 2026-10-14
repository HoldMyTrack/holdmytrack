package stats

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/httpapi"
)

// testTx is a REPEATABLE READ transaction on TEST_DATABASE_URL, rolled back when the test ends
// (skipped without one; docs/DEVELOPMENT.md). Other packages' tests share the database, so the
// test reads each value before and after seeding, in one snapshot, and checks the difference.
func testTx(t *testing.T) pgx.Tx {
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
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback(context.Background()) })
	return tx
}

func TestDemoUserID(t *testing.T) {
	if demoUserID != httpapi.DemoCustomerUserID {
		t.Fatalf("demoUserID %s, httpapi.DemoCustomerUserID %s", demoUserID, httpapi.DemoCustomerUserID)
	}
}

func TestTake(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	day := time.Date(2001, 2, 3, 0, 0, 0, 0, time.UTC)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	before, err := compute(ctx, tx, day)
	if err != nil {
		t.Fatal(err)
	}

	n := 0
	account := func(country string, lastSeen *time.Time, created time.Time) string {
		t.Helper()
		n++
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, country, last_seen_at, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
			fmt.Sprintf("stats-test-%d-%d@holdmytrack.invalid", time.Now().UnixNano(), n), country, lastSeen, created,
		).Scan(&id); err != nil {
			t.Fatalf("create account: %v", err)
		}
		return id
	}
	at := func(d time.Duration) *time.Time { v := day.Add(d); return &v }
	activity := func(userID, source string, created time.Time) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO activities (user_id, source, activity_type, started_at, timezone, created_at)
			VALUES ($1, $2, 'run', $3, 'UTC', $3) RETURNING id`, userID, source, created,
		).Scan(&id); err != nil {
			t.Fatalf("create activity: %v", err)
		}
		return id
	}

	// Five accounts in QQ, four in QZ: QQ keeps its key, QZ folds into other. Their last visits
	// fall on the day, six days before it, seven days before it, and never.
	var qq []string
	for i, seen := range []*time.Time{at(time.Hour), at(-6 * 24 * time.Hour), at(-7 * 24 * time.Hour), nil, nil} {
		created := day.Add(-48 * time.Hour)
		if i == 0 {
			created = day.Add(2 * time.Hour) // a signup on the day
		}
		qq = append(qq, account("QQ", seen, created))
	}
	var qz []string
	for range 4 {
		qz = append(qz, account("QZ", nil, day.Add(-48*time.Hour)))
	}
	deleted := account("QQ", at(time.Hour), day.Add(-48*time.Hour))
	exec(`UPDATE users SET deleted_at = NOW() WHERE id = $1`, deleted)

	var qqCountry, qzCountry int
	for code, id := range map[string]*int{"QQ": &qqCountry, "QZ": &qzCountry} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO admin_countries (code, name, geom) VALUES ($1, $1, ST_Multi(ST_MakeEnvelope(0, 0, 1, 1, 4326)))
			RETURNING id`, code).Scan(id); err != nil {
			t.Fatalf("create country: %v", err)
		}
	}

	// Activities: three of one account's on the day (two uploads, one sync), one each for two
	// more accounts the day before; the demo's and the deleted account's count nowhere.
	a1 := activity(qq[0], "upload", day.Add(3*time.Hour))
	activity(qq[0], "upload", day.Add(4*time.Hour))
	activity(qq[0], "healthconnect", day.Add(5*time.Hour))
	activity(qq[1], "recorded", day.Add(-time.Hour))
	activity(qz[0], "upload", day.Add(-time.Hour))
	activity(demoUserID, "upload", day.Add(3*time.Hour))
	activity(deleted, "upload", day.Add(3*time.Hour))
	exec(`INSERT INTO activity_country (activity_id, country_id) VALUES ($1, $2), ($1, $3)`, a1, qqCountry, qzCountry)

	// Photos: two rows over the same files (a copy) count their bytes once.
	photo := func(userID, activityID, key string, bytes int) {
		exec(`INSERT INTO activity_photos (user_id, activity_id, route_at, content_type, thumb_content_type, width, height, bytes, image_key)
			VALUES ($1, $2, $3, 'image/jpeg', 'image/jpeg', 10, 10, $4, $5)`, userID, activityID, day, bytes, key)
	}
	photo(qq[0], a1, "stats-test/a", 1000)
	photo(qq[0], a1, "stats-test/a", 1000)
	photo(qq[0], a1, "stats-test/b", 500)

	after, err := compute(ctx, tx, day)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"accounts":                           9,
		"signups":                            1,
		"active_1d":                          1,
		"active_7d":                          2,
		"active_30d":                         3,
		"activities":                         5,
		"activities_added":                   3,
		"activities_by_source.upload":        3,
		"activities_by_source.healthconnect": 1,
		"activities_by_source.recorded":      1,
		"activities_by_source.healthkit":     0,
		"activities_added_by_source.upload":  2,
		"photos":                             3,
		"storage_bytes.photos":               1500,
		"accounts_by_country.qq":             5,
		"accounts_by_country.qz":             0,
		"accounts_by_country.other":          4,
		"accounts_with.photos":               1,
		"activity_accounts_by_country.qq":    0, // one account: folded
		"activity_accounts_by_country.other": 2, // its one account, counted under each country
		"activities_by_country.other":        2,
	} {
		if got := after[key] - before[key]; got != want {
			t.Errorf("%s: went up by %v, want %v", key, got, want)
		}
	}
	if _, ok := after["accounts_by_country.qz"]; ok {
		t.Errorf("accounts_by_country.qz written, want it folded")
	}

	taken, err := Take(ctx, tx, day)
	if err != nil {
		t.Fatal(err)
	}
	if taken != len(after) {
		t.Errorf("Take wrote %d values, want %d", taken, len(after))
	}
	var stored float64
	if err := tx.QueryRow(ctx, `SELECT value FROM daily_stats WHERE day = $1 AND key = 'storage_bytes.photos'`, day).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != after["storage_bytes.photos"] {
		t.Errorf("stored storage_bytes.photos %v, want %v", stored, after["storage_bytes.photos"])
	}

	// Again for the same day: nothing changes.
	exec(`UPDATE activities SET source = 'healthkit' WHERE user_id = $1`, qq[0])
	if again, err := Take(ctx, tx, day); err != nil || again != 0 {
		t.Errorf("second Take: wrote %d, err %v; want 0, nil", again, err)
	}
	if err := tx.QueryRow(ctx, `SELECT value FROM daily_stats WHERE day = $1 AND key = 'storage_bytes.photos'`, day).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != after["storage_bytes.photos"] {
		t.Errorf("after a second Take, storage_bytes.photos %v, want %v", stored, after["storage_bytes.photos"])
	}
}
