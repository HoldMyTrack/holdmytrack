package bikepaths

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
// one — docs/DEVELOPMENT.md). A planet import deletes every way it doesn't see, other tests'
// included, so it runs here, with bike_paths locked against writes until the rollback and
// emptied first: the run sees only the test's own ways, and no other test sees what it did.
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
	for _, sql := range []string{`LOCK TABLE bike_paths IN EXCLUSIVE MODE`, `DELETE FROM bike_paths`} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return tx
}

// A refresh: an unchanged re-run changes nothing and leaves the tile version alone; a planet run
// deletes what it didn't see; a regional run deletes nothing; and a run that would delete too
// much deletes none.
func TestImportDeletes(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	base := time.Now().UnixNano()
	line := func(n int, name string) string {
		lat := -60.5 + float64(n)*0.01
		return fmt.Sprintf(`{"type":"Feature","id":"w%d","geometry":{"type":"LineString","coordinates":[[-40.5,%f],[-40.4,%f]]},"properties":{"highway":"cycleway","name":%q}}`,
			base+int64(n), lat, lat, name)
	}
	file := func(lines ...string) *strings.Reader { return strings.NewReader(strings.Join(lines, "\n") + "\n") }
	run := func(planet bool, maxDeleted float64, lines ...string) (ImportStats, error) {
		t.Helper()
		return importWays(ctx, tx, log, file(lines...), planet, maxDeleted)
	}
	stored := func(n int) bool {
		t.Helper()
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bike_paths WHERE osm_id = $1)`, base+int64(n)).Scan(&ok); err != nil {
			t.Fatalf("way %d: %v", n, err)
		}
		return ok
	}
	var userID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("bike-paths-import-%d@holdmytrack.invalid", base)).Scan(&userID); err != nil {
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
	if err != nil || stats.Imported != 3 || stats.Changed != 3 || stats.Deleted != 0 {
		t.Fatalf("first run: %+v, %v", stats, err)
	}
	if version() != v+1 {
		t.Errorf("first run: the tile version should move")
	}

	v = version()
	stats, err = run(true, 1, line(1, "One"), line(2, "Two"), line(3, "Three"))
	if err != nil || stats.Imported != 3 || stats.Changed != 0 || stats.Deleted != 0 {
		t.Fatalf("unchanged re-run: %+v, %v", stats, err)
	}
	if version() != v {
		t.Errorf("unchanged re-run: the tile version moved")
	}

	stats, err = run(true, 1, line(1, "One renamed"), line(2, "Two"))
	if err != nil || stats.Changed != 1 || stats.Deleted != 1 {
		t.Fatalf("a way renamed and one gone: %+v, %v", stats, err)
	}
	if !stored(1) || !stored(2) || stored(3) {
		t.Errorf("only the way gone should be deleted: %v %v %v", stored(1), stored(2), stored(3))
	}

	stats, err = run(false, 1, line(1, "One renamed"))
	if err != nil || stats.Deleted != 0 || !stored(2) {
		t.Errorf("a regional run deleted something: %+v, %v", stats, err)
	}

	v = version()
	stats, err = run(true, 0.01, line(1, "One renamed"))
	if !errors.Is(err, ErrTooManyDeleted) || stats.Deleted != 0 || !stored(2) {
		t.Errorf("deleting 1 of 2: %+v, %v; want ErrTooManyDeleted and nothing deleted", stats, err)
	}
	if version() != v {
		t.Errorf("a refused run that changed nothing moved the tile version")
	}
}
