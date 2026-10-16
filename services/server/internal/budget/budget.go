// Package budget is the daily R2 budget (IMPLEMENTATION.md §5.7, docs/SPEC.md FR-17, ADR-0044):
// a ceiling on what every account together may cost in object store requests in a UTC day.
// Each process counts its own requests (internal/storage's countingTransport) and adds them to
// the day's total in r2_ops; every process reads that total back to decide what to give up as
// the day's spend nears the budget: renders first, then anything that adds, and the reads the
// map shows last.
package budget

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/HoldMyTrack/platform/services/server/internal/metrics"
	"github.com/HoldMyTrack/platform/services/server/internal/storage"
)

// R2's list prices per request, in US dollars. The free monthly allowance is left out, so the
// spend counted is never less than the bill's.
const (
	classAPrice = 4.50 / 1e6
	classBPrice = 0.36 / 1e6
)

// Stage is how much of the service the day's spend has stopped, each including the ones
// before it.
type Stage int

const (
	Normal        Stage = iota
	RendersPaused       // Fog and Heatmap renders wait for the next day
	AddingPaused        // uploads, syncs, photos, avatars, Story copies and data downloads are refused
	ReadsPaused         // Fog and Heatmap tiles, photo images and download archives are refused
)

// The share of the budget at which each stage starts. Below 100% so that what's still running
// at the last stage can't take the day much past the budget.
const (
	rendersAt = 0.70
	addingAt  = 0.85
	readsAt   = 1.00
)

// spend is what a day's requests cost.
func spend(a, b int64) float64 {
	return float64(a)*classAPrice + float64(b)*classBPrice
}

// stageFor is the stage for a day whose spend is ratio of the budget.
func stageFor(ratio float64) Stage {
	switch {
	case ratio >= readsAt:
		return ReadsPaused
	case ratio >= addingAt:
		return AddingPaused
	case ratio >= rendersAt:
		return RendersPaused
	}
	return Normal
}

// day is the UTC day t falls on, as the date r2_ops keys it by.
func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// UntilReset is how long from now until the budget's day ends, for a Retry-After.
func UntilReset(now time.Time) time.Duration {
	return day(now).AddDate(0, 0, 1).Sub(now)
}

// flushInterval is how often a process adds its counts to the day's total, and refreshInterval
// how often a Gate reads the total back: together, how far behind the stages can run.
const (
	flushInterval   = 30 * time.Second
	refreshInterval = 30 * time.Second
)

// Flush adds the requests this process made since the last flush to today's total. Counts it
// couldn't store go back for the next one. A request made just before midnight and flushed just
// after counts for the new day: off by at most a flush's worth.
func Flush(ctx context.Context, pool *pgxpool.Pool) error {
	a, b := storage.TakeOps()
	if a == 0 && b == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO r2_ops (day, class, count)
		SELECT $1, c, n FROM unnest(ARRAY['A', 'B'], ARRAY[$2::bigint, $3::bigint]) AS t(c, n) WHERE n > 0
		ON CONFLICT (day, class) DO UPDATE SET count = r2_ops.count + EXCLUDED.count`,
		day(time.Now()), a, b)
	if err != nil {
		storage.ReturnOps(a, b)
		return fmt.Errorf("budget: flush: %w", err)
	}
	return nil
}

// RunFlusher flushes every flushInterval until ctx is cancelled, and once more then, so a
// process stopping doesn't lose its last counts.
func RunFlusher(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			done, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := Flush(done, pool); err != nil {
				log.Error("budget flush failed", "err", err)
			}
			cancel()
			return
		case <-t.C:
			if err := Flush(ctx, pool); err != nil {
				log.Error("budget flush failed", "err", err)
			}
		}
	}
}

// Ratio is the share of daily that today's spend has reached, as r2_ops records it.
func Ratio(ctx context.Context, pool *pgxpool.Pool, daily float64, now time.Time) (float64, error) {
	var a, b int64
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(sum(count) FILTER (WHERE class = 'A'), 0), coalesce(sum(count) FILTER (WHERE class = 'B'), 0)
		FROM r2_ops WHERE day = $1`, day(now)).Scan(&a, &b); err != nil {
		return 0, fmt.Errorf("budget: read today's requests: %w", err)
	}
	return spend(a, b) / daily, nil
}

// Gate answers what the day's spend has stopped, reading the total at most every
// refreshInterval. A nil Gate stops nothing, for a process or test without a budget. A failed
// read keeps the last answer: a database too troubled to answer this is the database's problem,
// not a reason to stop the service.
type Gate struct {
	pool  *pgxpool.Pool
	daily float64
	log   *slog.Logger

	mu      sync.Mutex
	stage   Stage
	checked time.Time
}

// NewGate is a Gate for a budget of daily US dollars a day.
func NewGate(pool *pgxpool.Pool, daily float64, log *slog.Logger) *Gate {
	return &Gate{pool: pool, daily: daily, log: log}
}

// Stage is the day's stage as of the last read.
func (g *Gate) Stage(ctx context.Context) Stage {
	if g == nil {
		return Normal
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.checked) < refreshInterval && day(now).Equal(day(g.checked)) {
		return g.stage
	}
	g.checked = now
	ratio, err := Ratio(ctx, g.pool, g.daily, now)
	if err != nil {
		g.log.Error("budget: read failed, keeping the last stage", "err", err)
		return g.stage
	}
	if s := stageFor(ratio); s != g.stage {
		g.log.Warn("budget: stage changed", "stage", int(s), "ratio", ratio)
		g.stage = s
	}
	return g.stage
}

// usedCollector reports today's spend as a share of the budget, holdmytrack_r2_budget_used_ratio,
// which the alerts at half and at the first stage read. `serve` registers it, so the series is
// sent once.
type usedCollector struct {
	pool  *pgxpool.Pool
	daily float64
	log   *slog.Logger
	desc  *prometheus.Desc
}

// RegisterCollector adds the budget gauge to metrics.Registry.
func RegisterCollector(pool *pgxpool.Pool, daily float64, log *slog.Logger) {
	metrics.Registry.MustRegister(&usedCollector{pool: pool, daily: daily, log: log,
		desc: prometheus.NewDesc("holdmytrack_r2_budget_used_ratio",
			"Today's (UTC) object store request spend as a share of the daily R2 budget.", nil, nil)})
}

func (c *usedCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *usedCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ratio, err := Ratio(ctx, c.pool, c.daily, time.Now())
	if err != nil {
		c.log.Error("metrics: read budget", "err", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, ratio)
}
