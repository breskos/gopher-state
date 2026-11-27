package workflow

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// RunBatch distributes independent runs numbered from 0 to runs-1 over
// contiguous, nonoverlapping ranges. Each worker calls run once with its
// half-open range [start, end). Worker results are returned in range order.
// A worker count of zero uses GOMAXPROCS, capped at the run count.
//
// Workers should keep their mutable state local and check ctx between runs.
// If any worker fails, RunBatch cancels the remaining workers and waits for
// them to finish before returning the cancellation cause.
func RunBatch[T any](ctx context.Context, runs, workers int, run func(ctx context.Context, start, end int) (T, error)) ([]T, error) {
	if runs <= 0 || workers < 0 || run == nil {
		return nil, fmt.Errorf("batch requires positive runs, nonnegative workers and a worker function")
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, runs)
	if workers == 1 {
		result, err := run(ctx, 0, runs)
		if err != nil {
			return nil, err
		}
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		return []T{result}, nil
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	results := make([]T, workers)
	var wg sync.WaitGroup
	size, remainder := runs/workers, runs%workers
	for worker := range workers {
		start := worker*size + min(worker, remainder)
		end := (worker+1)*size + min(worker+1, remainder)
		wg.Go(func() {
			if ctx.Err() != nil {
				return
			}
			result, err := run(ctx, start, end)
			if err != nil {
				cancel(err)
				return
			}
			results[worker] = result
		})
	}
	wg.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return results, nil
}
