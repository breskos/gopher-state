package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestSimulate(t *testing.T) {
	engine, err := loadModel(graph)
	if err != nil {
		t.Fatal(err)
	}
	base := settings{budgetCents: 1000, reserveCents: 200, maxMeters: 800, maxSeconds: 900, metersPerMinute: 60}
	for _, tt := range []struct {
		name                  string
		budget, meters, time  int
		raining               bool
		pick                  *uint64
		path                  []string
		wallet, walk, seconds int
	}{
		{"market lunch", 1000, 800, 900, false, pick(0), []string{"walk-to-market", "buy-lunch", "return-from-shop"}, 350, 600, 660},
		{"bakery by weight", 1000, 800, 900, false, pick(3), []string{"walk-to-bakery", "buy-pastry", "return-from-shop"}, 820, 240, 260},
		{"reserve forces cheaper fruit", 849, 800, 900, false, pick(0), []string{"walk-to-market", "buy-fruit", "return-from-shop"}, 599, 600, 630},
		{"lunch at exact money and time limits", 850, 600, 660, false, pick(0), []string{"walk-to-market", "buy-lunch", "return-from-shop"}, 200, 600, 660},
		{"shorter purchase fits deadline", 1000, 800, 640, false, pick(0), []string{"walk-to-market", "buy-fruit", "return-from-shop"}, 750, 600, 630},
		{"distance rules out market", 1000, 240, 900, false, nil, []string{"walk-to-bakery", "buy-pastry", "return-from-shop"}, 820, 240, 260},
		{"free walk when shopping exceeds budget", 300, 800, 900, false, nil, []string{"walk-to-park", "return-from-park"}, 300, 160, 160},
		{"rain blocks park", 300, 800, 900, true, nil, []string{"stay-home"}, 300, 0, 0},
		{"time rules out all trips", 1000, 800, 159, false, nil, []string{"stay-home"}, 1000, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := base
			s.budgetCents, s.maxMeters, s.maxSeconds, s.raining = tt.budget, tt.meters, tt.time, tt.raining
			draws := 0
			result, err := engine.simulate(s, func(limit uint64) uint64 {
				draws++
				if tt.pick == nil || limit != 4 {
					t.Fatalf("unexpected draw with limit %d", limit)
				}
				return *tt.pick
			}, true)
			if err != nil {
				t.Fatal(err)
			}
			if tt.pick != nil && draws != 1 {
				t.Errorf("weighted decision drew %d times, want 1", draws)
			}
			if result.state != "done" || result.wallet != tt.wallet || result.distance != tt.walk ||
				result.elapsed != tt.seconds || !slices.Equal(result.path, tt.path) {
				t.Fatalf("unexpected trip: %+v", result)
			}
		})
	}
}

func pick(value uint64) *uint64 { return &value }

func TestSimulationUsesJSONMetadata(t *testing.T) {
	updated := bytes.Replace(graph, []byte(`"priceCents": 650`), []byte(`"priceCents": 700`), 1)
	if bytes.Equal(updated, graph) {
		t.Fatal("test did not change the lunch price")
	}
	engine, err := loadModel(updated)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.simulate(settings{
		budgetCents: 875, reserveCents: 200, maxMeters: 800, maxSeconds: 900, metersPerMinute: 60,
	}, func(uint64) uint64 { return 0 }, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.wallet != 625 || !slices.Equal(result.path, []string{"walk-to-market", "buy-fruit", "return-from-shop"}) {
		t.Fatalf("price change did not affect the decision: %+v", result)
	}
}

func TestPersonCommand(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{[]string{"-max-meters", "240"}, []string{"walk-to-bakery", "Wallet: 820 cents", "Walked: 240 m", "Elapsed: 260 s"}},
		{[]string{"-runs", "25", "-workers", "4", "-budget", "300", "-rain"}, []string{"Runs: 25", "Home: 25", "Spent: 0 cents"}},
		{[]string{"-summary", "-max-meters", "240"}, []string{"Runs: 1", "Bakery: 1", "Spent: 180 cents"}},
	} {
		var out bytes.Buffer
		if err := run(tt.args, &out); err != nil {
			t.Fatal(err)
		}
		for _, want := range tt.want {
			if !strings.Contains(out.String(), want) {
				t.Errorf("run %v output %q does not contain %q", tt.args, out.String(), want)
			}
		}
	}
	for _, args := range [][]string{{"-speed", "0"}, {"-runs", "0"}, {"-runs", "3", "-workers", "-1"}, {"-unknown"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("run %v accepted invalid input", args)
		}
	}
}
