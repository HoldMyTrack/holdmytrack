package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// Shutdown waits for a request already running: it gets its response, and serveUntilDone
// returns only after it has.
func TestServeUntilDoneDrainsRunningRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		io.WriteString(w, "done")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() { returned <- serveUntilDone(ctx, srv, ln, 5*time.Second, slog.New(slog.DiscardHandler)) }()

	type result struct {
		body string
		err  error
	}
	resp := make(chan result, 1)
	go func() {
		r, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			resp <- result{err: err}
			return
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		resp <- result{string(b), err}
	}()

	<-started
	cancel()
	select {
	case err := <-returned:
		t.Fatalf("returned (%v) while a request was still running", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(finish)
	if r := <-resp; r.err != nil || r.body != "done" {
		t.Fatalf("response: %q, %v", r.body, r.err)
	}
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
}
