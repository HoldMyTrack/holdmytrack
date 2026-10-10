// Package parallel runs a bounded number of calls at once — for work that mostly waits on
// object storage (internal/fog's renders, internal/worker's account purge), where doing one
// call after another leaves the time almost all in round trips.
package parallel

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// ForEach runs fn(ctx, i) for every i in [0, n), at most limit at a time, and returns the
// first error. After an error no further calls start, and the ctx the running ones were given
// is cancelled; it waits for those to return before it does. A call that panics fails the same
// way, its panic and stack the error: a panic in a goroutine of its own can't be recovered by
// the caller (the worker's runJobSafely), and would end the process instead of the one job.
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
			if err := call(ctx, i, fn); err != nil {
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

// call is fn(ctx, i) with a panic turned into its error.
func call(ctx context.Context, i int, fn func(ctx context.Context, i int) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return fn(ctx, i)
}
