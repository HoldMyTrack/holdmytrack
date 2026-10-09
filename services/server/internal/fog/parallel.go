package fog

import (
	"context"
	"sync"
)

// renderParallelism bounds how many tiles (or masks) a render works on at once. The work is
// mostly waiting on object storage — a tile is one or more GETs and two PUTs, each a round
// trip to R2 — so running them one after another left a render at about 1.3 tiles a second
// on the production droplet (the 2026-10-08 load test), almost none of it CPU. Bounded rather
// than unbounded so memory stays predictable (IMPLEMENTATION.md §5.2): each in-flight tile
// holds a few decoded masks.
const renderParallelism = 16

// forEach runs fn(ctx, i) for every i in [0, n), at most limit at a time, and returns the
// first error. After an error no further calls start, and the ctx the running ones were given
// is cancelled; it waits for those to return before it does.
func forEach(ctx context.Context, n, limit int, fn func(ctx context.Context, i int) error) error {
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
