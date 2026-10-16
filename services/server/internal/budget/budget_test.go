package budget

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/HoldMyTrack/platform/services/server/internal/db"
	"github.com/HoldMyTrack/platform/services/server/internal/storage"
)

func TestSpend(t *testing.T) {
	if got := spend(1_000_000, 10_000_000); math.Abs(got-(4.50+3.60)) > 1e-9 {
		t.Errorf("spend = %v, want 8.10", got)
	}
}

func TestStageFor(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		want  Stage
	}{
		{0, Normal}, {0.69, Normal}, {0.70, RendersPaused}, {0.84, RendersPaused},
		{0.85, AddingPaused}, {0.99, AddingPaused}, {1.0, ReadsPaused}, {3, ReadsPaused},
	} {
		if got := stageFor(tc.ratio); got != tc.want {
			t.Errorf("stageFor(%v) = %d, want %d", tc.ratio, got, tc.want)
		}
	}
}

func TestUntilReset(t *testing.T) {
	now := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	if got := UntilReset(now); got != 90*time.Minute {
		t.Errorf("UntilReset = %v, want 1h30m", got)
	}
	if got := UntilReset(now.In(time.FixedZone("UTC+3", 3*3600))); got != 90*time.Minute {
		t.Errorf("UntilReset in another zone = %v, want the UTC day's 1h30m", got)
	}
}

func TestNilGateStopsNothing(t *testing.T) {
	var g *Gate
	if s := g.Stage(context.Background()); s != Normal {
		t.Errorf("nil Gate: stage %d", s)
	}
}

// A flush adds this process's counts to today's row; the Gate reads the total back as a stage.
// Runs against TEST_DATABASE_URL (skipped without one), on today's rows, which it restores.
func TestFlushAndGate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	today := day(time.Now())
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM r2_ops WHERE day = $1`, today) })
	if _, err := pool.Exec(ctx, `DELETE FROM r2_ops WHERE day = $1`, today); err != nil {
		t.Fatal(err)
	}

	storage.TakeOps()
	storage.ReturnOps(100_000, 1_000_000) // $0.45 + $0.36
	if err := Flush(ctx, pool); err != nil {
		t.Fatal(err)
	}
	storage.ReturnOps(100_000, 0) // another $0.45, onto the same rows
	if err := Flush(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ratio, err := Ratio(ctx, pool, 1.0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(ratio-1.26) > 1e-9 {
		t.Errorf("ratio %v, want 1.26 of a $1 budget", ratio)
	}
	log := slog.New(slog.DiscardHandler)
	if s := NewGate(pool, 1.0, log).Stage(ctx); s != ReadsPaused {
		t.Errorf("$1.26 of $1: stage %d, want ReadsPaused", s)
	}
	if s := NewGate(pool, 1.5, log).Stage(ctx); s != RendersPaused {
		t.Errorf("$1.26 of $1.50: stage %d, want RendersPaused", s)
	}
	if s := NewGate(pool, 2.0, log).Stage(ctx); s != Normal {
		t.Errorf("$1.26 of $2: stage %d, want Normal", s)
	}
}
