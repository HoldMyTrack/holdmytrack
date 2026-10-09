// Package parallel runs a bounded number of calls at once — for work that mostly waits on
// object storage (internal/fog's renders, internal/worker's account purge), where doing one
// call after another leaves the time almost all in round trips.
package parallel

import (
	"context"
	"sync"
)

// ForEach runs fn(ctx, i) for every i in [0, n), at most limit at a time, and returns the
// first error. After an error no further calls start, and the ctx the running ones were given
// is cancelled; it waits for those to return before it does.
func ForEach(ctx context.Context, n, limit int, fn func(ctx context.Context, i int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	sem := make(chan struct{}, limit)
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(ctx, i); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	// A parent context cancelled before any call failed (a worker shutdown).
	return context.Cause(ctx)
}
