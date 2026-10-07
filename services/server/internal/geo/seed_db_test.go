package geo

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
)

// testTx is a transaction on TEST_DATABASE_URL, rolled back when the test ends (skipped without
// one — docs/DEVELOPMENT.md). The load replaces every outline and every activity's matches, so
// it runs here: no other test sees what it did.
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback(context.Background()) })
	return tx
}

// A load replaces the outlines, gives a country with no regions one region that is all of it,
// leaves out a region whose country isn't there, re-matches activities against the full-detail
// pieces and moves the tile version; loading the same extract again does nothing, and one with
// too few outlines is refused.
func TestLoadBoundaries(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	box := func(lon1, lat1, lon2, lat2 float64) string {
		var hex string
		if err := tx.QueryRow(ctx, `SELECT encode(ST_AsBinary(ST_MakeEnvelope($1, $2, $3, $4)), 'hex')`,
			lon1, lat1, lon2, lat2).Scan(&hex); err != nil {
			t.Fatal(err)
		}
		return hex
	}
	// Two countries side by side in the South Atlantic, where no real outline is; AA has a
	// region over its west half, BB none, and ZZ-1's country isn't in the file. CC is smaller
	// than the tiles' simplifying can keep, so it keeps its full outline.
	file := func(rows ...string) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(strings.Join(rows, "\n") + "\n"))
		gz.Close()
		return &buf
	}
	extract := func() *bytes.Buffer {
		return file(
			"country,AA,AA,,Aland,"+box(-41, -61, -40, -60),
			"country,BB,BB,,\"Bland, the\","+box(-40, -61, -39, -60),
			"country,CC,CC,,Cland,"+box(-38, -61, -37.999, -60.999),
			"region,div-aa-1,AA,AA-1,West Aland,"+box(-41, -61, -40.5, -60),
			"region,div-zz-1,ZZ,ZZ-1,Nowhere,"+box(-30, -61, -29, -60),
		)
	}

	var userID, activityID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("geo-seed-%d@holdmytrack.invalid", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// From West Aland into Bland, never touching Aland's east half on its own.
	if err := tx.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, activity_type, started_at, trajectory, timezone)
		VALUES ($1, 'upload', 'walking', now(), ST_GeomFromText('LINESTRING M(-40.8 -60.5 0, -39.5 -60.5 60)', 4326), 'UTC')
		RETURNING id`, userID).Scan(&activityID); err != nil {
		t.Fatalf("create activity: %v", err)
	}
	version := func() int64 {
		var v int64
		if err := tx.QueryRow(ctx, `SELECT map_version FROM users WHERE id = $1`, userID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	matched := func(sql string) string {
		var s string
		if err := tx.QueryRow(ctx, sql, activityID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	before := version()
	stats, err := loadBoundaries(ctx, tx, log, extract(), "test-1.csv.gz", false, 3, 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stats.Countries != 3 || stats.Regions != 1 || stats.SkippedRegions != 1 || stats.WholeCountryRegions != 2 {
		t.Errorf("stats %+v", stats)
	}
	if got := matched(`SELECT string_agg(c.code || ' ' || c.name, ', ' ORDER BY c.code)
		FROM activity_country ac JOIN admin_countries c ON c.id = ac.country_id WHERE ac.activity_id = $1`); got != "AA Aland, BB Bland, the" {
		t.Errorf("countries %q", got)
	}
	if got := matched(`SELECT string_agg(r.overture_id || ' ' || COALESCE(r.code, '-'), ', ' ORDER BY r.overture_id)
		FROM activity_region ar JOIN admin_regions r ON r.id = ar.region_id WHERE ar.activity_id = $1`); got != "country:BB -, div-aa-1 AA-1" {
		t.Errorf("regions %q", got)
	}
	var tiny int
	if err := tx.QueryRow(ctx, `SELECT ST_NPoints(c.geom) + ST_NPoints(r.geom) FROM admin_countries c
		JOIN admin_regions r ON r.country_id = c.id WHERE c.code = 'CC'`).Scan(&tiny); err != nil || tiny != 10 {
		t.Errorf("Cland's outlines: %d points, %v", tiny, err)
	}
	if v := version(); v != before+1 {
		t.Errorf("map_version %d, want %d", v, before+1)
	}

	stats, err = loadBoundaries(ctx, tx, log, extract(), "test-1.csv.gz", false, 3, 1)
	if err != nil || !stats.Unchanged || version() != before+1 {
		t.Errorf("the same extract again: %+v, %v, version %d", stats, err, version())
	}

	if _, err := loadBoundaries(ctx, tx, log, extract(), "test-2.csv.gz", false, 4, 1); err == nil || !strings.Contains(err.Error(), "cut short") {
		t.Errorf("too few countries: %v", err)
	}
}

// A track stored continuing past 180 across the antimeridian (ingest's unwrapLons) matches the
// countries either side of it, and not one the same parallel crosses on the far side of the
// world.
func TestMatchAcrossTheAntimeridian(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	box := func(lon1, lat1, lon2, lat2 float64) string {
		var hex string
		if err := tx.QueryRow(ctx, `SELECT encode(ST_AsBinary(ST_MakeEnvelope($1, $2, $3, $4)), 'hex')`,
			lon1, lat1, lon2, lat2).Scan(&hex); err != nil {
			t.Fatal(err)
		}
		return hex
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte(strings.Join([]string{
		"country,EE,EE,,East of it," + box(179, -61, 180, -60),
		"country,WW,WW,,West of it," + box(-180, -61, -179, -60),
		"country,FF,FF,,Far away," + box(0, -61, 1, -60),
	}, "\n") + "\n"))
	gz.Close()

	var userID, activityID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("geo-am-%d@holdmytrack.invalid", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, activity_type, started_at, trajectory, timezone)
		VALUES ($1, 'upload', 'sailing', now(), ST_GeomFromText('LINESTRING M(179.5 -60.5 0, 180.5 -60.5 60)', 4326), 'UTC')
		RETURNING id`, userID).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBoundaries(ctx, tx, log, &buf, "am.csv.gz", true, 3, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	var got string
	if err := tx.QueryRow(ctx, `SELECT string_agg(c.code, ',' ORDER BY c.code)
		FROM activity_country ac JOIN admin_countries c ON c.id = ac.country_id WHERE ac.activity_id = $1`, activityID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "EE,WW" {
		t.Errorf("matched %q, want EE,WW", got)
	}
}
