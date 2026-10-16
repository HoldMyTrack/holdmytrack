package metrics

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/HoldMyTrack/platform/services/server/internal/db"
)

// The gauges report the newest day's rows and nothing from an older day. Runs in a transaction
// on TEST_DATABASE_URL, rolled back at the end (skipped without one; docs/DEVELOPMENT.md).
func TestStatsCollector(t *testing.T) {
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
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `
		INSERT INTO daily_stats (day, key, value) VALUES
			('2999-01-01', 'accounts', 12), ('2999-01-01', 'active_30d', 7),
			('2998-12-31', 'accounts', 11), ('2998-12-31', 'signups', 3)`); err != nil {
		t.Fatal(err)
	}

	ch := make(chan prometheus.Metric, 16)
	newStatsCollector(tx, slog.New(slog.DiscardHandler)).Collect(ch)
	close(ch)
	got := map[string]float64{}
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			t.Fatal(err)
		}
		got[d.GetLabel()[0].GetValue()] = d.GetGauge().GetValue()
	}
	want := map[string]float64{"accounts": 12, "active_30d": 7}
	if len(got) != len(want) || got["accounts"] != 12 || got["active_30d"] != 7 {
		t.Errorf("got %v, want %v", got, want)
	}
}
