package gopherstate

import (
	"fmt"
	"reflect"
)

// DFA is a compiled deterministic finite automaton. Its methods can be called
// concurrently. Its zero value rejects every input.
type DFA[S comparable] struct {
	start       int
	final       []bool
	rows        []dfaRow[S]
	checkSymbol bool
}

// CompileDFA validates and snapshots def. Identical transitions are allowed.
// Conflicting targets for the same state and symbol return ErrInvalidDefinition.
func CompileDFA[S comparable](def Definition[S]) (*DFA[S], error) {
	final, err := acceptingStates(def)
	if err != nil {
		return nil, err
	}
	d := &DFA[S]{
		start:       def.Start,
		final:       final,
		rows:        make([]dfaRow[S], def.StateCount),
		checkSymbol: mayContainInterface(reflect.TypeFor[S]()),
	}
	for i, edge := range def.Transitions {
		row := &d.rows[edge.From]
		if target, exists := row.step(edge.Symbol); exists {
			if target != edge.To {
				return nil, fmt.Errorf("%w: transition %d conflicts with another transition from state %d", ErrInvalidDefinition, i, edge.From)
			}
			continue
		}
		row.add(edge.Symbol, edge.To)
	}
	return d, nil
}

// Start returns the initial state.
func (d *DFA[S]) Start() int { return d.start }

// IsAccepting reports whether state is an accepting state.
func (d *DFA[S]) IsAccepting(state int) bool {
	return state >= 0 && state < len(d.final) && d.final[state]
}

// Step looks up a single transition. It returns false for a missing transition
// or an out-of-range state.
func (d *DFA[S]) Step(state int, symbol S) (int, bool) {
	if state < 0 || state >= len(d.rows) {
		return 0, false
	}
	if d.checkSymbol && !validSymbol(symbol) {
		return 0, false
	}
	return d.rows[state].step(symbol)
}

// dfaRow stores the most common small case inline. Larger rows use a map for
// additional transitions.
type dfaRow[S comparable] struct {
	symbol S
	target int // One-based so an empty row needs no separate flag.
	extra  map[S]int
}

func (r *dfaRow[S]) step(symbol S) (int, bool) {
	if r.target != 0 && r.symbol == symbol {
		return r.target - 1, true
	}
	next, ok := r.extra[symbol]
	return next, ok
}

func (r *dfaRow[S]) add(symbol S, target int) {
	if r.target == 0 {
		r.symbol, r.target = symbol, target+1
		return
	}
	if r.extra == nil {
		r.extra = make(map[S]int)
	}
	r.extra[symbol] = target
}

// Match reports whether the DFA accepts the entire input.
func (d *DFA[S]) Match(input []S) bool {
	if len(d.rows) == 0 {
		return false
	}
	state := d.start
	for _, symbol := range input {
		if d.checkSymbol && !validSymbol(symbol) {
			return false
		}
		next, ok := d.rows[state].step(symbol)
		if !ok {
			return false
		}
		state = next
	}
	return d.IsAccepting(state)
}

func mayContainInterface(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Array:
		return t.Len() > 0 && mayContainInterface(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if mayContainInterface(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}
