package gopherstate

import (
	"fmt"
	"math/bits"
)

// BitsetMatcher is an immutable execution plan for an NFA of up to 64 states.
// Its Match method can be called concurrently. Its zero value rejects all inputs.
type BitsetMatcher[S comparable] struct {
	rows        []bitsetRow[S]
	initial     uint64
	accepting   uint64
	checkSymbol bool
}

type bitsetRow[S comparable] struct {
	symbol S
	first  uint64
	extra  map[S]uint64
}

// NewBitsetMatcher precomputes epsilon-closed transition masks. This can speed
// up small, highly branched NFAs. It returns ErrStateLimit for more than 64 states.
func (n *NFA[S]) NewBitsetMatcher() (*BitsetMatcher[S], error) {
	if len(n.final) > 64 {
		return nil, fmt.Errorf("%w: bitset matching supports up to 64 states", ErrStateLimit)
	}
	m := &BitsetMatcher[S]{rows: make([]bitsetRow[S], len(n.final)), checkSymbol: n.checkSymbol}
	var closure [64]uint64
	for state := range n.final {
		active := uint64(1) << state
		pending := active
		for pending != 0 {
			from := bits.TrailingZeros64(pending)
			pending &= pending - 1
			for _, target := range n.epsilon[from] {
				bit := uint64(1) << target
				pending |= bit &^ active
				active |= bit
			}
		}
		closure[state] = active
		if n.final[state] {
			m.accepting |= uint64(1) << state
		}
	}
	if len(n.initial) != 0 {
		m.initial = closure[n.initial[0]]
	}
	for state, source := range n.rows {
		row := &m.rows[state]
		row.symbol = source.symbol
		for _, target := range source.targets {
			row.first |= closure[target]
		}
		if len(source.extra) != 0 {
			row.extra = make(map[S]uint64, len(source.extra))
			for symbol, targets := range source.extra {
				var mask uint64
				for _, target := range targets {
					mask |= closure[target]
				}
				row.extra[symbol] = mask
			}
		}
	}
	return m, nil
}

// Match reports whether the NFA accepts the entire input, without allocating
// working memory.
func (m *BitsetMatcher[S]) Match(input []S) bool {
	active := m.initial
	for _, symbol := range input {
		if m.checkSymbol && !validSymbol(symbol) {
			return false
		}
		var next uint64
		for active != 0 {
			row := &m.rows[bits.TrailingZeros64(active)]
			active &= active - 1
			if row.first != 0 && row.symbol == symbol {
				next |= row.first
			} else if row.extra != nil {
				next |= row.extra[symbol]
			}
		}
		if next == 0 {
			return false
		}
		active = next
	}
	return active&m.accepting != 0
}
