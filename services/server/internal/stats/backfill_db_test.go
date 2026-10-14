package stats

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

// The backfill fills an unmeasured original (once for each activity sharing it) and an
// unmeasured tile from the bucket's listing, leaves a size already recorded alone, and a second
// run writes nothing. It writes to TEST_DATABASE_URL (skipped without one), so the account it
// makes is deleted again; the objects are in an in-memory bucket of its own.
func TestBackfillObjectBytes(t *testing.T) {
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
	mem := storagetest.New()
	srv := httptest.NewServer(mem)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("stats-backfill-%d@holdmytrack.invalid", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })

	shared := "raw/" + userID + "/day.gpx"
	measured := "raw/" + userID + "/other.gpx"
	mem.Put(shared, make([]byte, 700))
	mem.Put(measured, make([]byte, 900))
	activity := func(key string, size *int64) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO activities (user_id, source, activity_type, started_at, timezone, raw_payload_key, raw_bytes)
			VALUES ($1, 'upload', 'run', NOW(), 'UTC', $2, $3) RETURNING id`, userID, key, size).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	recorded := int64(123)
	piece1, piece2 := activity(shared, nil), activity(shared, nil)
	other := activity(measured, &recorded)

	fogKey := fmt.Sprintf("fog/%s/14/10/20.png", userID)
	heatKey := fmt.Sprintf("heatmap/%s/14/10/20.png", userID)
	mem.Put(fogKey, make([]byte, 40))
	mem.Put(heatKey, make([]byte, 60))
	if _, err := pool.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, object_key, heatmap_object_key, dirty)
		VALUES ($1, 14, 10, 20, $2, $3, false)`, userID, fogKey, heatKey); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	if _, err := BackfillObjectBytes(ctx, pool, store, log); err != nil {
		t.Fatal(err)
	}
	rawBytes := func(id string) int64 {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT raw_bytes FROM activities WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("raw_bytes of %s: %v", id, err)
		}
		return n
	}
	if a, b, c := rawBytes(piece1), rawBytes(piece2), rawBytes(other); a != 700 || b != 700 || c != 123 {
		t.Errorf("raw_bytes %d, %d, %d; want 700, 700 and the recorded 123", a, b, c)
	}
	var fogBytes, heatBytes int
	if err := pool.QueryRow(ctx, `SELECT bytes, heatmap_bytes FROM fog_tiles WHERE user_id = $1`, userID).Scan(&fogBytes, &heatBytes); err != nil {
		t.Fatal(err)
	}
	if fogBytes != 40 || heatBytes != 60 {
		t.Errorf("tile sizes %d, %d; want 40, 60", fogBytes, heatBytes)
	}

	if n, err := BackfillObjectBytes(ctx, pool, store, log); err != nil || n != 0 {
		t.Errorf("second run: wrote %d, err %v; want 0, nil", n, err)
	}
}
