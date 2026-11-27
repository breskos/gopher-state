package main

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

var sampleSettings = settings{
	budgetCents: 1000, reserveCents: 200, maxMeters: 800, maxSeconds: 900, metersPerMinute: 60,
}

func TestBatchMatchesIndividualRuns(t *testing.T) {
	model, err := loadModel(graph)
	if err != nil {
		t.Fatal(err)
	}
	const runs, seed = 1003, 42
	var want totals
	for id := range runs {
		rng := rand.New(rand.NewPCG(seed, uint64(id)))
		result, err := model.simulate(sampleSettings, rng.Uint64N, true)
		if err != nil {
			t.Fatal(err)
		}
		switch result.path[0] {
		case "walk-to-market":
			want.market++
			want.spent += 650
			want.distance += 600
			want.elapsed += 660
		case "walk-to-bakery":
			want.bakery++
			want.spent += 180
			want.distance += 240
			want.elapsed += 260
		default:
			t.Fatalf("unexpected route: %v", result.path)
		}
	}
	if want.market == 0 || want.bakery == 0 {
		t.Fatal("seed did not cover both routes")
	}
	for _, workers := range []int{0, 1, 2, 4, 8} {
		got, err := model.batch(t.Context(), sampleSettings, seed, runs, workers)
		if err != nil || got != want {
			t.Errorf("workers=%d: got %+v, %v; want %+v", workers, got, err, want)
		}
	}
}

var tripSink trip
var totalsSink totals

func BenchmarkFullTrip(b *testing.B) {
	model, err := loadModel(graph)
	if err != nil {
		b.Fatal(err)
	}
	draw := func(uint64) uint64 { return 0 }
	for _, trace := range []bool{true, false} {
		b.Run(fmt.Sprintf("trace=%t", trace), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tripSink, err = model.simulate(sampleSettings, draw, trace)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPersonBatch(b *testing.B) {
	model, err := loadModel(graph)
	if err != nil {
		b.Fatal(err)
	}
	const runs = 16_384
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				totalsSink, err = model.batch(b.Context(), sampleSettings, 1, runs, workers)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.N)*runs/b.Elapsed().Seconds(), "runs/s")
		})
	}
}
