package gopherstate

import (
	"fmt"
	"slices"
)

// LazyDFALimits bounds cached subsets (including the initial subset) and symbol
// transitions (including transitions that reject). Both limits must be positive.
type LazyDFALimits struct {
	MaxStates      int
	MaxTransitions int
}

// LazyMatcher memoizes DFA transitions encountered while matching an NFA.
// Use one matcher per goroutine. Cached transitions survive across Match calls.
type LazyMatcher[S comparable] struct {
	nfa         *NFA[S]
	limits      LazyDFALimits
	states      []lazyState[S]
	ids         map[string]int
	transitions int
	first       []int
	second      []int
	seen        []bool
	key         []byte
}

type lazyState[S comparable] struct {
	subset    []int
	accepting bool
	row       dfaRow[S]
}

// NewLazyMatcher prepares a matcher with a bounded cache. On a cache miss
// that cannot fit, it continues with NFA simulation. It never rejects input
// because of the cache budget.
func (n *NFA[S]) NewLazyMatcher(limits LazyDFALimits) (*LazyMatcher[S], error) {
	if limits.MaxStates <= 0 || limits.MaxTransitions <= 0 {
		return nil, fmt.Errorf("%w: lazy DFA cache limits must be positive", ErrInvalidDefinition)
	}
	initial := slices.Clone(n.initial)
	slices.Sort(initial)
	return &LazyMatcher[S]{
		nfa: n, limits: limits,
		states: []lazyState[S]{{subset: initial, accepting: n.acceptsSubset(initial)}},
		ids:    map[string]int{subsetKey(initial): 0},
		first:  make([]int, 0, len(n.final)), second: make([]int, 0, len(n.final)),
		seen: make([]bool, len(n.final)),
	}, nil
}

// Match reports whether the NFA accepts the entire input. The zero value
// rejects every input.
func (m *LazyMatcher[S]) Match(input []S) bool {
	if m.nfa == nil {
		return false
	}
	current := 0
	for offset, symbol := range input {
		if m.nfa.checkSymbol && !validSymbol(symbol) {
			return false
		}
		state := &m.states[current]
		next, ok := state.row.step(symbol)
		if !ok {
			if m.transitions == m.limits.MaxTransitions {
				return m.nfa.matchFrom(input[offset:], state.subset, m.first, m.second, m.seen)
			}
			subset := m.nfa.advance(state.subset, symbol, m.first, m.seen)
			next = -1
			if len(subset) != 0 {
				slices.Sort(subset)
				m.key = appendSubsetKey(m.key[:0], subset)
				next, ok = m.ids[string(m.key)]
				if !ok {
					if len(m.states) == m.limits.MaxStates {
						return m.nfa.matchFrom(input[offset+1:], subset, m.second, m.first, m.seen)
					}
					next = len(m.states)
					m.ids[string(m.key)] = next
					m.states = append(m.states, lazyState[S]{
						subset: slices.Clone(subset), accepting: m.nfa.acceptsSubset(subset),
					})
				}
			}
			// Store one-based subset IDs so a rejecting edge uses target zero,
			// which dfaRow can cache in its inline slot.
			m.states[current].row.add(symbol, next+1)
			m.transitions++
		} else {
			next--
		}
		if next < 0 {
			return false
		}
		current = next
	}
	return m.states[current].accepting
}
