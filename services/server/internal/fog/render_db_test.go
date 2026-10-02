package fog

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
)

// Reads TEST_DATABASE_URL and skips without it, like internal/httpapi's database tests
// (docs/DEVELOPMENT.md).
func testAccount(t *testing.T) (*pgxpool.Pool, string) {
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
	var userID string
	email := fmt.Sprintf("fog-test-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete account: %v", err)
		}
	})
	return pool, userID
}

func tileDirty(t *testing.T, pool *pgxpool.Pool, userID string, zoom, x, y int) bool {
	t.Helper()
	var dirty bool
	if err := pool.QueryRow(context.Background(),
		`SELECT dirty FROM fog_tiles WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, zoom, x, y).Scan(&dirty); err != nil {
		t.Fatalf("read z%d/%d/%d: %v", zoom, x, y, err)
	}
	return dirty
}

// A render clears the tile's dirty flag only if nothing marked it again while it was drawn —
// an activity deleted mid-render must not leave its old mask in the stored tile for good —
// and always leaves its parent dirty, so the pyramid above survives an interrupted pass.
func TestUpsertTileRenderedKeepsAMidRenderMark(t *testing.T) {
	pool, userID := testAccount(t)
	ctx := context.Background()
	const z, x, y = Zoom, 100, 201
	if _, err := pool.Exec(ctx,
		`INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty) VALUES ($1, $2, $3, $4, true)`,
		userID, z, x, y); err != nil {
		t.Fatal(err)
	}

	gen, err := tileDirtyGen(ctx, pool, userID, z, x, y)
	if err != nil {
		t.Fatal(err)
	}
	// What MarkFogTilesDirty does when a delete lands while the render is compositing.
	if _, err := pool.Exec(ctx,
		`UPDATE fog_tiles SET dirty = true, dirty_gen = dirty_gen + 1 WHERE user_id = $1 AND zoom = $2 AND tile_x = $3 AND tile_y = $4`,
		userID, z, x, y); err != nil {
		t.Fatal(err)
	}
	if err := upsertTileRendered(ctx, pool, userID, z, x, y, gen, "fog", "heat"); err != nil {
		t.Fatal(err)
	}
	if !tileDirty(t, pool, userID, z, x, y) {
		t.Error("tile marked mid-render was cleared")
	}
	if !tileDirty(t, pool, userID, z-1, x/2, y/2) {
		t.Error("parent not marked dirty")
	}

	gen, err = tileDirtyGen(ctx, pool, userID, z, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertTileRendered(ctx, pool, userID, z, x, y, gen, "fog", "heat"); err != nil {
		t.Fatal(err)
	}
	if tileDirty(t, pool, userID, z, x, y) {
		t.Error("tile rendered with nothing marking it since stayed dirty")
	}
}
