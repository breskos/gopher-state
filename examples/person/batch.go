package main

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/breskos/gopher-state/workflow"
)

type totals struct {
	market, bakery, park, home int
	spent, distance, elapsed   int64
}

func (m *personModel) batch(ctx context.Context, s settings, seed uint64, runs, workers int) (totals, error) {
	if err := s.validate(); err != nil {
		return totals{}, err
	}
	// Activity metadata is bounded at load time. This upper bound guarantees
	// that summing distances, time and purchases cannot overflow int64.
	if int64(runs) > math.MaxInt64/1_000_000_000 {
		return totals{}, fmt.Errorf("too many trips to aggregate")
	}
	parts, err := workflow.RunBatch(ctx, runs, workers, func(ctx context.Context, start, end int) (totals, error) {
		var part totals
		source := rand.NewPCG(seed, 0)
		draw := rand.New(source).Uint64N
		for id := start; id < end; id++ {
			if err := ctx.Err(); err != nil {
				return totals{}, err
			}
			source.Seed(seed, uint64(id))
			result, err := m.simulate(s, draw, false)
			if err != nil {
				return totals{}, fmt.Errorf("run %d: %w", id, err)
			}
			switch result.outing {
			case "market":
				part.market++
			case "bakery":
				part.bakery++
			case "park":
				part.park++
			default:
				part.home++
			}
			part.spent += int64(s.budgetCents - result.wallet)
			part.distance += int64(result.distance)
			part.elapsed += int64(result.elapsed)
		}
		return part, nil
	})
	if err != nil {
		return totals{}, err
	}
	var total totals
	for _, part := range parts {
		total.market += part.market
		total.bakery += part.bakery
		total.park += part.park
		total.home += part.home
		total.spent += part.spent
		total.distance += part.distance
		total.elapsed += part.elapsed
	}
	return total, nil
}
