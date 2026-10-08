// Package metrics is what `serve` and `work` expose for monitoring (IMPLEMENTATION.md §5.8,
// docs/DEPLOY.md §13): Prometheus metrics on a listener of their own, METRICS_ADDR, which the
// Grafana Alloy service scrapes over the Docker network. It's a separate listener rather than
// a route on the API's, because Caddy sends every path it doesn't serve itself to the API, so
// a /metrics route there would be public.
//
// Label values are always from a fixed set (a route pattern, a job kind, a failure code),
// never an id or anything a request supplied: every distinct value is a series, and the free
// Grafana Cloud tier counts them.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds every metric below plus the Go runtime's and the process's own. A registry
// of our own rather than the client library's global one, so nothing a dependency registers
// ends up in the series count unasked.
var Registry = prometheus.NewRegistry()

var (
	// HTTPRequests counts the API's responses by route (the ServeMux pattern that matched,
	// "unmatched" for none) and status class, "2xx" to "5xx". The class rather than the code
	// keeps the series down to what the alerts look at.
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "holdmytrack_http_requests_total",
		Help: "API responses by route pattern and status class.",
	}, []string{"route", "class"})

	// HTTPDuration is the API's time to respond, by route.
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "holdmytrack_http_request_duration_seconds",
		Help:    "Time to respond, by route pattern.",
		Buckets: []float64{0.025, 0.1, 0.25, 1, 2.5, 10},
	}, []string{"route"})

	// HTTPPanics counts handler panics the API recovered from and answered with a 500.
	HTTPPanics = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "holdmytrack_http_panics_total",
		Help: "Handler panics recovered by the API.",
	})

	// JobsFinished counts jobs the worker finished, by kind, outcome ("done" or "failed") and,
	// for a failed ingest, its jobs.error_code: "internal" is ours, every other code is the
	// user's file. A failed job of another kind has code "internal" too.
	JobsFinished = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "holdmytrack_jobs_finished_total",
		Help: "Jobs finished by the worker, by kind, outcome and failure code.",
	}, []string{"kind", "outcome", "code"})

	// JobDuration is how long a job ran, by kind.
	JobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "holdmytrack_job_duration_seconds",
		Help:    "Time a job ran, by kind.",
		Buckets: []float64{0.1, 0.5, 2, 10, 60, 300},
	}, []string{"kind"})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequests, HTTPDuration, HTTPPanics, JobsFinished, JobDuration,
	)
}

// StatusClass is "2xx" for 200–299 and so on.
func StatusClass(status int) string {
	return strconv.Itoa(status/100) + "xx"
}

// JobKinds are the jobs.kind values worker.runJob handles. The queue gauges report each one
// even with nothing waiting, so a series exists at 0 rather than appearing only once a job
// is stuck.
var JobKinds = []string{"ingest", "unpack", "render_fog", "edit_track", "reprivacy", "story_copy"}

// queueCollector reads the queue from the database on every scrape: how many jobs are
// runnable now, and how long the oldest has waited, by kind. `serve` registers it rather
// than `work`, so the numbers keep coming while the worker is down, which is when they grow.
type queueCollector struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	jobs   *prometheus.Desc
	oldest *prometheus.Desc
}

// RegisterQueue adds the queue gauges to Registry.
func RegisterQueue(pool *pgxpool.Pool, log *slog.Logger) {
	Registry.MustRegister(&queueCollector{
		pool: pool,
		log:  log,
		jobs: prometheus.NewDesc("holdmytrack_jobs_runnable",
			"Pending jobs whose run_after has passed, by kind.", []string{"kind"}, nil),
		oldest: prometheus.NewDesc("holdmytrack_jobs_oldest_runnable_age_seconds",
			"How long the oldest runnable job has waited, by kind; 0 with none.", []string{"kind"}, nil),
	})
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.jobs
	ch <- c.oldest
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type row struct {
		count int64
		age   float64
	}
	byKind := map[string]row{}
	// idx_jobs_runnable covers the WHERE, so this reads only the queue, not the job history.
	rows, err := c.pool.Query(ctx, `
		SELECT kind, count(*), EXTRACT(EPOCH FROM NOW() - min(run_after))::float8
		FROM jobs WHERE state = 'pending' AND run_after <= NOW()
		GROUP BY kind`)
	if err != nil {
		// No sample at all, rather than zeros that would read as an empty queue.
		c.log.Error("metrics: read job queue", "err", err)
		return
	}
	for rows.Next() {
		var kind string
		var r row
		if err := rows.Scan(&kind, &r.count, &r.age); err != nil {
			c.log.Error("metrics: read job queue", "err", err)
			rows.Close()
			return
		}
		byKind[kind] = r
	}
	if err := rows.Err(); err != nil {
		c.log.Error("metrics: read job queue", "err", err)
		return
	}
	kinds := append([]string{}, JobKinds...)
	for kind := range byKind {
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range kinds {
		r := byKind[kind]
		ch <- prometheus.MustNewConstMetric(c.jobs, prometheus.GaugeValue, float64(r.count), kind)
		ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, r.age, kind)
	}
}

// Serve answers GET /metrics on addr until ctx is cancelled. An empty addr turns it off.
func Serve(ctx context.Context, addr string, log *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	go func() {
		log.Info("metrics: listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics: listen", "err", err)
		}
	}()
}
