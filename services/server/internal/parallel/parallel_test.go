package parallel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestForEachRunsEveryIndexWithinTheLimit(t *testing.T) {
	var running, peak atomic.Int32
	seen := make([]atomic.Bool, 50)
	err := ForEach(context.Background(), len(seen), 4, func(ctx context.Context, i int) error {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		seen[i].Store(true)
		running.Add(-1)
		return nil
	})
	if err != nil {
		t.Fatalf("forEach: %v", err)
	}
	for i := range seen {
		if !seen[i].Load() {
			t.Errorf("index %d never ran", i)
		}
	}
	if p := peak.Load(); p > 4 || p < 2 {
		t.Errorf("peak concurrency %d, want 2..4", p)
	}
}

func TestForEachStopsAtTheFirstError(t *testing.T) {
	boom := errors.New("boom")
	var started atomic.Int32
	err := ForEach(context.Background(), 1000, 2, func(ctx context.Context, i int) error {
		started.Add(1)
		if i == 3 {
			return boom
		}
		select {
		case <-ctx.Done(): // the failure cancels the ones still running
		case <-time.After(time.Millisecond):
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := started.Load(); n > 10 {
		t.Errorf("%d calls started after the failure, want it to stop early", n)
	}
}

func TestForEachReportsACancelledParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ForEach(ctx, 5, 2, func(context.Context, int) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
