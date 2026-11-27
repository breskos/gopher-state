package gopherstate

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// ErrStateLimit identifies a determinization that would exceed its state budget.
var ErrStateLimit = errors.New("determinization state limit exceeded")

// Determinize constructs an equivalent DFA by exploring reachable subsets.
// maxStates must be positive and bounds the number of generated DFA states,
// including the initial state. The resulting DFA uses implicit rejection for
// missing transitions. Generated state IDs beyond the start state are unspecified.
// A zero-value NFA becomes a one-state rejecting DFA.
func (n *NFA[S]) Determinize(maxStates int) (*DFA[S], error) {
	if maxStates <= 0 {
		return nil, fmt.Errorf("%w: maxStates must be positive", ErrInvalidDefinition)
	}
	initial := slices.Clone(n.initial)
	slices.Sort(initial)
	subsets := [][]int{initial}
	ids := map[string]int{subsetKey(initial): 0}
	dfa := &DFA[S]{checkSymbol: n.checkSymbol}
	seen := make([]bool, len(n.final))

	for i := 0; i < len(subsets); i++ {
		current := subsets[i]
		accepting := false
		destinations := make(map[S][]int)
		for _, state := range current {
			accepting = accepting || n.final[state]
			row := &n.rows[state]
			if len(row.targets) > 0 {
				destinations[row.symbol] = append(destinations[row.symbol], row.targets...)
			}
			for symbol, targets := range row.extra {
				destinations[symbol] = append(destinations[symbol], targets...)
			}
		}
		var row dfaRow[S]
		for symbol, targets := range destinations {
			unique := targets[:0]
			for _, target := range targets {
				if !seen[target] {
					seen[target] = true
					unique = append(unique, target)
				}
			}
			unique = n.close(unique, seen)
			for _, target := range unique {
				seen[target] = false
			}
			slices.Sort(unique)
			key := subsetKey(unique)
			id, known := ids[key]
			if !known {
				if len(subsets) == maxStates {
					return nil, fmt.Errorf("%w: maxStates=%d", ErrStateLimit, maxStates)
				}
				id = len(subsets)
				ids[key] = id
				subsets = append(subsets, unique)
			}
			row.add(symbol, id)
		}
		dfa.final = append(dfa.final, accepting)
		dfa.rows = append(dfa.rows, row)
	}
	return dfa, nil
}

func subsetKey(states []int) string {
	encoded := make([]byte, 0, len(states))
	for _, state := range states {
		encoded = binary.AppendUvarint(encoded, uint64(state))
	}
	return string(encoded)
}
