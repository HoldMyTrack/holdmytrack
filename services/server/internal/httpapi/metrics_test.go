package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
)

func newMetricsTestServer() *Server {
	s := &Server{log: slog.New(slog.DiscardHandler), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/test/ok/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /v1/test/teapot", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	s.mux.HandleFunc("GET /v1/test/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	return s
}

func TestServeHTTPCountsByRoutePattern(t *testing.T) {
	s := newMetricsTestServer()
	ok := metrics.HTTPRequests.WithLabelValues("GET /v1/test/ok/{id}", "2xx")
	before := testutil.ToFloat64(ok)
	for _, id := range []string{"a", "b"} {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/test/ok/"+id, nil))
	}
	if got := testutil.ToFloat64(ok) - before; got != 2 {
		t.Errorf("two requests to one route with different ids: counted %v, want 2 under the pattern", got)
	}

	teapot := metrics.HTTPRequests.WithLabelValues("GET /v1/test/teapot", "4xx")
	before = testutil.ToFloat64(teapot)
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/test/teapot", nil))
	if got := testutil.ToFloat64(teapot) - before; got != 1 {
		t.Errorf("a 418: counted %v under 4xx, want 1", got)
	}

	unmatched := metrics.HTTPRequests.WithLabelValues("unmatched", "4xx")
	before = testutil.ToFloat64(unmatched)
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/no/such/path", nil))
	if got := testutil.ToFloat64(unmatched) - before; got != 1 {
		t.Errorf("an unknown path: counted %v under unmatched, want 1", got)
	}
}

func TestServeHTTPRecoversPanic(t *testing.T) {
	s := newMetricsTestServer()
	panics := testutil.ToFloat64(metrics.HTTPPanics)
	errs := metrics.HTTPRequests.WithLabelValues("GET /v1/test/panic", "5xx")
	before := testutil.ToFloat64(errs)

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/v1/test/panic", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", w.Code)
	}
	if got := testutil.ToFloat64(metrics.HTTPPanics) - panics; got != 1 {
		t.Errorf("panics counted %v, want 1", got)
	}
	if got := testutil.ToFloat64(errs) - before; got != 1 {
		t.Errorf("the panic counted %v times as a 5xx response, want 1", got)
	}
}
