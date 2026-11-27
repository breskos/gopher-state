package workflow_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

func TestRunBatchPartitions(t *testing.T) {
	for _, workers := range []int{0, 1, 2, 4, 30} {
		parts, err := workflow.RunBatch(t.Context(), 17, workers, func(_ context.Context, start, end int) ([]int, error) {
			ids := make([]int, 0, end-start)
			for id := start; id < end; id++ {
				ids = append(ids, id)
			}
			return ids, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var actual []int
		for _, part := range parts {
			actual = append(actual, part...)
		}
		expected := make([]int, 17)
		for id := range expected {
			expected[id] = id
		}
		if !slices.Equal(actual, expected) {
			t.Errorf("%d workers: run IDs = %v", workers, actual)
		}
	}
}

func TestRunBatchCancels(t *testing.T) {
	failure := errors.New("simulation failed")
	started := make(chan struct{})
	stopped := make(chan struct{})
	_, err := workflow.RunBatch(t.Context(), 2, 2, func(ctx context.Context, start, _ int) (int, error) {
		if start == 0 {
			close(started)
			<-ctx.Done()
			close(stopped)
			return 0, ctx.Err()
		}
		<-started
		return 0, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("batch returned %v, want original error", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("batch returned before the other worker stopped")
	}

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(failure)
	_, err = workflow.RunBatch(ctx, 1, 1, func(context.Context, int, int) (int, error) {
		t.Fatal("already cancelled batch started a worker")
		return 0, nil
	})
	if !errors.Is(err, failure) {
		t.Fatalf("already cancelled batch returned %v", err)
	}
}
