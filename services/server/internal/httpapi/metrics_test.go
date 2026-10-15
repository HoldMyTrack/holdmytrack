package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAppClient(t *testing.T) {
	long := "HoldMyTrack-Android/0.13 (62da50b) " + strings.Repeat("x", 100)
	for _, tc := range []struct{ ua, want string }{
		{"HoldMyTrack-Android/0.13 (62da50b)", "HoldMyTrack-Android/0.13 (62da50b)"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15", ""},
		{"", ""},
		{"okhttp/4.12.0", ""},
		{long, long[:64]},
	} {
		if got := appClient(tc.ua); got != tc.want {
			t.Errorf("appClient(%q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
}

func TestServeHTTPLogsTheAppBuildOnAFailure(t *testing.T) {
	var buf bytes.Buffer
	s := newMetricsTestServer()
	s.log = slog.New(slog.NewJSONHandler(&buf, nil))
	s.mux.HandleFunc("GET /v1/test/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, path := range []string{"/v1/test/fail", "/v1/test/panic"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("User-Agent", "HoldMyTrack-Android/0.13 (62da50b)")
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/test/teapot", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want one per failed request:\n%s", len(lines), buf.String())
	}
	for i, msg := range []string{"request failed", "handler panicked"} {
		var line map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &line); err != nil {
			t.Fatal(err)
		}
		if line["msg"] != msg || line["client"] != "HoldMyTrack-Android/0.13 (62da50b)" {
			t.Errorf("line %d = %s, want %q with the app's build", i, lines[i], msg)
		}
	}
}
