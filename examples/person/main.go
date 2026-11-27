// Command person runs an event-driven simulation of a person's shopping trip.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed person.json
var graph []byte

type settings struct {
	budgetCents, reserveCents int
	maxMeters, maxSeconds     int
	metersPerMinute           int
	raining                   bool
}

type activity struct {
	meters, seconds, priceCents int
}

type trip struct {
	state                     string
	wallet, distance, elapsed int
	path                      []string
	outing                    string
}

type personModel struct {
	engine     *workflow.Engine
	activities map[string]activity
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("person", flag.ContinueOnError)
	flags.SetOutput(out)
	var s settings
	flags.IntVar(&s.budgetCents, "budget", 1000, "starting wallet in cents")
	flags.IntVar(&s.reserveCents, "reserve", 200, "cents to keep unspent")
	flags.IntVar(&s.maxMeters, "max-meters", 800, "maximum round-trip walking distance in meters")
	flags.IntVar(&s.maxSeconds, "max-seconds", 900, "time available in seconds")
	flags.IntVar(&s.metersPerMinute, "speed", 60, "walking speed in meters per minute")
	flags.BoolVar(&s.raining, "rain", false, "whether it is raining")
	seed := flags.Uint64("seed", 1, "seed for reproducible route selection")
	runs := flags.Int("runs", 1, "number of independent simulations")
	workers := flags.Int("workers", 0, "parallel workers (0 uses Go's available parallelism)")
	summary := flags.Bool("summary", false, "print totals without recording individual paths")
	if err := flags.Parse(args); err != nil {
		return err
	}

	model, err := loadModel(graph)
	if err != nil {
		return err
	}
	if *runs != 1 || *summary {
		total, err := model.batch(context.Background(), s, *seed, *runs, *workers)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Runs: %d | Market: %d | Bakery: %d | Park: %d | Home: %d\n",
			*runs, total.market, total.bakery, total.park, total.home)
		fmt.Fprintf(out, "Spent: %d cents | Walked: %d m | Elapsed: %d s\n",
			total.spent, total.distance, total.elapsed)
		return nil
	}
	if err := s.validate(); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(*seed, 0))
	result, err := model.simulate(s, rng.Uint64N, true)
	if err != nil {
		return err
	}
	for _, transition := range result.path {
		fmt.Fprintln(out, transition)
	}
	fmt.Fprintf(out, "Final state: %s | Wallet: %d cents | Walked: %d m | Elapsed: %d s\n",
		result.state, result.wallet, result.distance, result.elapsed)
	return nil
}

func readActivity(engine *workflow.Engine, id string) (activity, error) {
	data, ok := engine.Metadata(id)
	if !ok {
		return activity{}, fmt.Errorf("unknown activity %q", id)
	}
	var a activity
	fields := []struct {
		key string
		dst *int
	}{{"meters", &a.meters}, {"seconds", &a.seconds}, {"priceCents", &a.priceCents}}
	for _, field := range fields {
		raw, exists := data[field.key]
		if !exists {
			continue
		}
		if err := json.Unmarshal(raw, field.dst); err != nil {
			return activity{}, fmt.Errorf("%s.%s: %w", id, field.key, err)
		}
		if *field.dst < 0 || *field.dst > 1_000_000 {
			return activity{}, fmt.Errorf("%s.%s is outside the simulation range", id, field.key)
		}
	}
	return a, nil
}

func loadModel(document []byte) (*personModel, error) {
	engine, err := workflow.ReadJSON(bytes.NewReader(document), 16<<10)
	if err != nil {
		return nil, err
	}
	activities := make(map[string]activity)
	for _, id := range []string{"walk-to-market", "walk-to-bakery", "walk-to-park", "buy-lunch", "buy-fruit", "buy-pastry"} {
		a, err := readActivity(engine, id)
		if err != nil {
			return nil, err
		}
		activities[id] = a
	}
	return &personModel{engine: engine, activities: activities}, nil
}

func (s settings) validate() error {
	if s.budgetCents < 0 || s.reserveCents < 0 || s.maxMeters < 0 || s.maxSeconds < 0 || s.metersPerMinute <= 0 {
		return fmt.Errorf("budget, reserve, distance and time must be nonnegative, and speed must be positive")
	}
	return nil
}

func (m *personModel) simulate(s settings, draw workflow.Draw, trace bool) (trip, error) {
	activities := m.activities

	// The application calculates facts from the simulation's units and limits.
	// The JSON graph decides which transitions those facts enable.
	walkTime := func(meters int) int {
		seconds := meters * 60
		wholeSeconds := seconds / s.metersPerMinute
		if seconds%s.metersPerMinute != 0 {
			wholeSeconds++
		}
		return wholeSeconds
	}
	fits := func(meters, seconds, price int) bool {
		return meters <= s.maxMeters && seconds <= s.maxSeconds && price <= s.budgetCents-s.reserveCents
	}
	routeFits := func(route, purchase string) bool {
		walk, buy := activities[route], activities[purchase]
		return fits(2*walk.meters, 2*walkTime(walk.meters)+buy.seconds, buy.priceCents)
	}
	parkMeters := activities["walk-to-park"].meters
	facts := workflow.Facts{
		"marketPossible": workflow.Bool(routeFits("walk-to-market", "buy-lunch") || routeFits("walk-to-market", "buy-fruit")),
		"bakeryPossible": workflow.Bool(routeFits("walk-to-bakery", "buy-pastry")),
		"parkPossible":   workflow.Bool(2*parkMeters <= s.maxMeters && 2*walkTime(parkMeters) <= s.maxSeconds),
		"raining":        workflow.Bool(s.raining),
	}

	result := trip{state: m.engine.Start(), wallet: s.budgetCents}
	if trace {
		result.path = make([]string, 0, 3)
	}
	advance := func(event string) error {
		move, ok, err := m.engine.Step(result.state, event, facts, draw)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no transition for event %q from %q", event, result.state)
		}
		a := activities[move.Transition]
		result.state = move.To
		if trace {
			result.path = append(result.path, move.Transition)
		}
		result.distance += a.meters
		result.elapsed += walkTime(a.meters) + a.seconds
		result.wallet -= a.priceCents
		return nil
	}

	if err := advance("plan"); err != nil {
		return result, err
	}
	result.outing = result.state
	if result.state == "done" {
		return result, nil
	}
	returnMeters := result.distance
	if result.state == "market" || result.state == "bakery" {
		delete(facts, "marketPossible")
		delete(facts, "bakeryPossible")
		delete(facts, "parkPossible")
		facts["spendable"] = workflow.Quantity(float64(result.wallet-s.reserveCents), "EUR-cent")
		facts["shoppingTime"] = workflow.Quantity(float64(s.maxSeconds-result.elapsed-walkTime(returnMeters)), "s")
		for _, offer := range [...]struct{ shop, price, duration, id string }{
			{"market", "lunchPrice", "lunchDuration", "buy-lunch"},
			{"market", "fruitPrice", "fruitDuration", "buy-fruit"},
			{"bakery", "pastryPrice", "pastryDuration", "buy-pastry"},
		} {
			if offer.shop != result.state {
				continue
			}
			purchase := activities[offer.id]
			facts[offer.price] = workflow.Quantity(float64(purchase.priceCents), "EUR-cent")
			facts[offer.duration] = workflow.Quantity(float64(purchase.seconds), "s")
		}
		if err := advance("shop"); err != nil {
			return result, err
		}
	}
	if err := advance("return"); err != nil {
		return result, err
	}
	result.distance += returnMeters
	result.elapsed += walkTime(returnMeters)
	return result, nil
}
