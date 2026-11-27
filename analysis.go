package gopherstate

// Visit identifies a transition at a given input offset. A symbol
// transition goes from Offset to Offset+1. An epsilon transition stays at Offset.
type Visit struct {
	Offset int
	From   int
	To     int
}

// RunStats summarizes one run. For an NFA, a state is counted once per input
// offset, even when multiple paths reach it. Edge counts include alternatives
// explored during the run, including branches that subsequently reject.
type RunStats struct {
	Accepted           bool
	Consumed           int
	StateVisits        int
	SymbolTransitions  int
	EpsilonTransitions int
	PeakActive         int
}

// Observer receives optional events during Analyze. Callbacks are synchronous.
// At most one State callback is made for each state at a given offset.
type Observer struct {
	State   func(offset, state int)
	Symbol  func(Visit)
	Epsilon func(Visit)
}

// Analyze runs the DFA with optional observation. Match is the fast path for
// callers who only need the acceptance result.
func (d *DFA[S]) Analyze(input []S, observer Observer) RunStats {
	return analyzeDFA(input, observer, len(d.final), d.start, d.Step, d.IsAccepting)
}

func analyzeDFA[S comparable](input []S, observer Observer, count, start int, step func(int, S) (int, bool), accepting func(int) bool) RunStats {
	var stats RunStats
	if count == 0 {
		return stats
	}
	current := start
	stats.PeakActive = 1
	stats.visitState(observer, 0, current)
	for offset, symbol := range input {
		next, ok := step(current, symbol)
		if !ok {
			return stats
		}
		stats.SymbolTransitions++
		if observer.Symbol != nil {
			observer.Symbol(Visit{offset, current, next})
		}
		stats.Consumed++
		stats.visitState(observer, stats.Consumed, next)
		current = next
	}
	stats.Accepted = accepting(current)
	return stats
}

// Analyze runs the NFA and observes its active states and examined edges.
// Each edge entry is counted once per active source and input offset.
// Duplicate edges in the definition count separately. Epsilon cycles are
// visited without recursively enumerating paths.
func (n *NFA[S]) Analyze(input []S, observer Observer) RunStats {
	var stats RunStats
	if len(n.initial) == 0 {
		return stats
	}
	current := make([]int, 0, len(n.final))
	next := make([]int, 0, len(n.final))
	seen := make([]bool, len(n.final))
	start := n.initial[0]
	current = append(current, start)
	seen[start] = true
	stats.visitState(observer, 0, start)
	current = n.observeClosure(current, seen, observer, &stats)
	for offset, symbol := range input {
		if n.checkSymbol && !validSymbol(symbol) {
			return stats
		}
		for _, from := range current {
			for _, to := range n.rows[from].step(symbol) {
				stats.SymbolTransitions++
				if observer.Symbol != nil {
					observer.Symbol(Visit{offset, from, to})
				}
				if !seen[to] {
					seen[to] = true
					next = append(next, to)
					stats.visitState(observer, offset+1, to)
				}
			}
		}
		if len(next) == 0 {
			return stats
		}
		stats.Consumed++
		next = n.observeClosure(next, seen, observer, &stats)
		current, next = next, current[:0]
	}
	for _, state := range current {
		if n.final[state] {
			stats.Accepted = true
			break
		}
	}
	return stats
}

func (n *NFA[S]) observeClosure(states []int, seen []bool, observer Observer, stats *RunStats) []int {
	for i := 0; i < len(states); i++ {
		from := states[i]
		for _, to := range n.epsilon[from] {
			stats.EpsilonTransitions++
			if observer.Epsilon != nil {
				observer.Epsilon(Visit{stats.Consumed, from, to})
			}
			if !seen[to] {
				seen[to] = true
				states = append(states, to)
				stats.visitState(observer, stats.Consumed, to)
			}
		}
	}
	for _, state := range states {
		seen[state] = false
	}
	stats.PeakActive = max(stats.PeakActive, len(states))
	return states
}

func (s *RunStats) visitState(observer Observer, offset, state int) {
	s.StateVisits++
	if observer.State != nil {
		observer.State(offset, state)
	}
}
