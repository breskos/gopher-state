package gopherstate

import (
	"fmt"
	"reflect"
)

// EpsilonTransition moves from From to To without consuming input.
type EpsilonTransition struct {
	From int
	To   int
}

// NFADefinition extends a definition with optional epsilon transitions.
type NFADefinition[S comparable] struct {
	Definition[S]
	Epsilon []EpsilonTransition
}

// NFA is a compiled nondeterministic finite automaton. Its methods can be
// called concurrently. Its zero value rejects every input.
type NFA[S comparable] struct {
	final       []bool
	rows        []nfaRow[S]
	epsilon     [][]int
	initial     []int
	checkSymbol bool
}

type nfaRow[S comparable] struct {
	symbol  S
	targets []int
	extra   map[S][]int
}

func (r *nfaRow[S]) add(symbol S, target int) {
	if len(r.targets) == 0 || r.symbol == symbol {
		r.symbol = symbol
		r.targets = append(r.targets, target)
		return
	}
	if r.extra == nil {
		r.extra = make(map[S][]int)
	}
	r.extra[symbol] = append(r.extra[symbol], target)
}

func (r *nfaRow[S]) step(symbol S) []int {
	if len(r.targets) != 0 && r.symbol == symbol {
		return r.targets
	}
	if r.extra == nil {
		return nil
	}
	return r.extra[symbol]
}

// CompileNFA validates and snapshots def. Multiple targets for a state and
// symbol are allowed. Epsilon cycles are supported.
func CompileNFA[S comparable](def NFADefinition[S]) (*NFA[S], error) {
	for i, edge := range def.Epsilon {
		if edge.From < 0 || edge.From >= def.StateCount || edge.To < 0 || edge.To >= def.StateCount {
			return nil, fmt.Errorf("%w: epsilon transition %d has out-of-range states (%d, %d)", ErrInvalidDefinition, i, edge.From, edge.To)
		}
	}
	final, err := acceptingStates(def.Definition)
	if err != nil {
		return nil, err
	}
	n := &NFA[S]{
		final: final, rows: make([]nfaRow[S], def.StateCount),
		epsilon: make([][]int, def.StateCount), checkSymbol: mayContainInterface(reflect.TypeFor[S]()),
	}
	for _, edge := range def.Transitions {
		n.rows[edge.From].add(edge.Symbol, edge.To)
	}
	for _, edge := range def.Epsilon {
		n.epsilon[edge.From] = append(n.epsilon[edge.From], edge.To)
	}
	seen := make([]bool, def.StateCount)
	seen[def.Start] = true
	n.initial = n.close([]int{def.Start}, seen)
	return n, nil
}

// Match reports whether any path accepts the entire input. For automata with
// more than 64 states, use NewMatcher to avoid allocating working memory on
// every call.
func (n *NFA[S]) Match(input []S) bool {
	const small = 64
	if len(n.final) <= small {
		var first, second [small]int
		var seen [small]bool
		return n.match(input, first[:0:len(n.final)], second[:0:len(n.final)], seen[:len(n.final)])
	}
	return n.NewMatcher().Match(input)
}

// NFAMatcher holds reusable working memory for one NFA. Use a separate
// matcher per goroutine. A match starts at the initial state on every call.
type NFAMatcher[S comparable] struct {
	nfa    *NFA[S]
	first  []int
	second []int
	seen   []bool
}

// NewMatcher creates a matcher with preallocated working memory.
func (n *NFA[S]) NewMatcher() *NFAMatcher[S] {
	size := len(n.final)
	return &NFAMatcher[S]{
		nfa: n, first: make([]int, 0, size), second: make([]int, 0, size), seen: make([]bool, size),
	}
}

// Match reports whether any path accepts the entire input. Its zero value
// rejects every input. A matcher created by NewMatcher allocates no memory per call.
func (m *NFAMatcher[S]) Match(input []S) bool {
	return m.nfa != nil && m.nfa.match(input, m.first, m.second, m.seen)
}

func (n *NFA[S]) match(input []S, first, second []int, seen []bool) bool {
	return n.matchFrom(input, n.initial, first, second, seen)
}

func (n *NFA[S]) matchFrom(input []S, current, next, spare []int, seen []bool) bool {
	for _, symbol := range input {
		if n.checkSymbol && !validSymbol(symbol) {
			return false
		}
		next = n.advance(current, symbol, next, seen)
		if len(next) == 0 {
			return false
		}
		current, next, spare = next, spare[:0], next
	}
	return n.acceptsSubset(current)
}

func (n *NFA[S]) advance(current []int, symbol S, next []int, seen []bool) []int {
	for _, state := range current {
		for _, target := range n.rows[state].step(symbol) {
			if !seen[target] {
				seen[target] = true
				next = append(next, target)
			}
		}
	}
	next = n.close(next, seen)
	for _, state := range next {
		seen[state] = false
	}
	return next
}

func (n *NFA[S]) acceptsSubset(states []int) bool {
	for _, state := range states {
		if n.final[state] {
			return true
		}
	}
	return false
}

func (n *NFA[S]) close(states []int, seen []bool) []int {
	for i := 0; i < len(states); i++ {
		for _, target := range n.epsilon[states[i]] {
			if !seen[target] {
				seen[target] = true
				states = append(states, target)
			}
		}
	}
	return states
}
