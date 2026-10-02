package spots

import (
	"context"
	"errors"
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
// one, like internal/httpapi's database tests — docs/DEVELOPMENT.md). A planet import retires
// every place it doesn't see, other tests' included, so it runs here, with spots locked against
// writes until the rollback and every place already in the database retired first: the run sees
// only the test's own places, and no other test sees what it did.
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
	for _, sql := range []string{
		`LOCK TABLE spots IN EXCLUSIVE MODE`,
		`UPDATE spots SET retired_at = now() WHERE retired_at IS NULL`,
	} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return tx
}

// A refresh (ADR-0027): an unchanged re-run changes nothing and leaves the tile version alone; a
// planet run retires what it didn't see, keeping its captures, and brings it back when it
// returns; a regional run retires nothing; and a run that would retire too much retires none.
func TestImportRetires(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	base := time.Now().UnixNano()
	line := func(n int, name string) string {
		return fmt.Sprintf(`{"type":"Feature","id":"n%d","geometry":{"type":"Point","coordinates":[-40.5,%f]},"properties":{"tourism":"viewpoint","name":%q}}`,
			base+int64(n), -60.5+float64(n)*0.01, name)
	}
	file := func(lines ...string) *strings.Reader { return strings.NewReader(strings.Join(lines, "\n") + "\n") }
	run := func(planet bool, maxRetired float64, lines ...string) (ImportStats, error) {
		t.Helper()
		return importPlaces(ctx, tx, log, file(lines...), planet, maxRetired)
	}
	retired := func(n int) bool {
		t.Helper()
		var at *time.Time
		if err := tx.QueryRow(ctx, `SELECT retired_at FROM spots WHERE osm_type = 'node' AND osm_id = $1`, base+int64(n)).Scan(&at); err != nil {
			t.Fatalf("place %d: %v", n, err)
		}
		return at != nil
	}
	var userID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("spots-import-%d@holdmytrack.invalid", base)).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	version := func() int64 {
		t.Helper()
		var v int64
		if err := tx.QueryRow(ctx, `SELECT map_version FROM users WHERE id = $1`, userID).Scan(&v); err != nil {
			t.Fatalf("map_version: %v", err)
		}
		return v
	}

	v := version()
	stats, err := run(true, 1, line(1, "One"), line(2, "Two"), line(3, "Three"))
	if err != nil || stats.Imported != 3 || stats.Changed != 3 || stats.Retired != 0 {
		t.Fatalf("first run: %+v, %v", stats, err)
	}
	if version() != v+1 {
		t.Errorf("first run: the tile version should move")
	}

	v = version()
	stats, err = run(true, 1, line(1, "One"), line(2, "Two"), line(3, "Three"))
	if err != nil || stats.Imported != 3 || stats.Changed != 0 || stats.Retired != 0 {
		t.Fatalf("unchanged re-run: %+v, %v", stats, err)
	}
	if version() != v {
		t.Errorf("unchanged re-run: the tile version moved")
	}

	if _, err := tx.Exec(ctx, `INSERT INTO spot_captures (user_id, spot_id)
		SELECT $1, id FROM spots WHERE osm_type = 'node' AND osm_id = $2`, userID, base+3); err != nil {
		t.Fatalf("capture: %v", err)
	}
	stats, err = run(true, 1, line(1, "One renamed"), line(2, "Two"))
	if err != nil || stats.Changed != 1 || stats.Retired != 1 {
		t.Fatalf("a place renamed and one gone: %+v, %v", stats, err)
	}
	if retired(1) || retired(2) || !retired(3) {
		t.Errorf("only the place gone should be retired: %v %v %v", retired(1), retired(2), retired(3))
	}
	var captures int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM spot_captures WHERE user_id = $1`, userID).Scan(&captures); err != nil || captures != 1 {
		t.Errorf("the retired place's capture: %d (%v), want 1", captures, err)
	}

	stats, err = run(false, 1, line(1, "One renamed"))
	if err != nil || stats.Retired != 0 || retired(2) {
		t.Errorf("a regional run retired something: %+v, %v", stats, err)
	}

	stats, err = run(true, 1, line(1, "One renamed"), line(2, "Two"), line(3, "Three"))
	if err != nil || stats.Changed != 1 || stats.Retired != 0 || retired(3) {
		t.Errorf("a place back in OSM: %+v, %v, retired %v", stats, err, retired(3))
	}

	v = version()
	stats, err = run(true, 0.01, line(1, "One renamed"))
	if !errors.Is(err, ErrTooManyRetired) || stats.Retired != 0 || retired(2) || retired(3) {
		t.Errorf("retiring 2 of 3: %+v, %v; want ErrTooManyRetired and nothing retired", stats, err)
	}
	if version() != v {
		t.Errorf("a refused run that changed nothing moved the tile version")
	}
}