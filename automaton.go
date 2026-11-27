// Package gopherstate compiles and runs finite automata over comparable symbols.
//
// Use [CompileDFA] for deterministic transitions and [CompileNFA] for multiple
// possible targets or transitions that consume no input. Compilation validates
// the definition and copies the state and transition data. A compiled automaton
// can then be shared across goroutines.
//
// A match consumes the entire input. If a transition is missing, its path
// rejects. An NFA accepts when any path ends in an accepting state after
// consuming all symbols and following any remaining epsilon transitions.
package gopherstate

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrInvalidDefinition identifies malformed automaton definitions.
var ErrInvalidDefinition = errors.New("invalid automaton definition")

// Transition consumes Symbol to move from From to To.
type Transition[S comparable] struct {
	From   int
	Symbol S
	To     int
}

// Definition describes an automaton with states numbered from 0 to StateCount-1.
// Missing transitions reject input. The zero value of S is an ordinary symbol.
type Definition[S comparable] struct {
	StateCount  int
	Start       int
	Accepting   []int
	Transitions []Transition[S]
}

func acceptingStates[S comparable](def Definition[S]) ([]bool, error) {
	valid := func(state int) bool { return state >= 0 && state < def.StateCount }
	if !valid(def.Start) {
		return nil, fmt.Errorf("%w: start state %d outside [0, %d)", ErrInvalidDefinition, def.Start, def.StateCount)
	}
	for _, state := range def.Accepting {
		if !valid(state) {
			return nil, fmt.Errorf("%w: accepting state %d out of range", ErrInvalidDefinition, state)
		}
	}
	checkSymbol := mayContainInterface(reflect.TypeFor[S]())
	for i, edge := range def.Transitions {
		if !valid(edge.From) || !valid(edge.To) {
			return nil, fmt.Errorf("%w: transition %d has out-of-range states (%d, %d)", ErrInvalidDefinition, i, edge.From, edge.To)
		}
		if (checkSymbol && !validSymbol(edge.Symbol)) || (!checkSymbol && edge.Symbol != edge.Symbol) {
			return nil, fmt.Errorf("%w: transition %d has an unhashable or non-reflexive symbol", ErrInvalidDefinition, i)
		}
	}
	accepting := make([]bool, def.StateCount)
	for _, state := range def.Accepting {
		accepting[state] = true
	}
	return accepting, nil
}

func validSymbol[S comparable](symbol S) bool {
	value := reflect.ValueOf(symbol)
	return (!value.IsValid() || value.Comparable()) && symbol == symbol
}
