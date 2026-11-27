package gopherstate

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrTableLimit means the dense transition table exceeds its configured budget.
var ErrTableLimit = errors.New("byte DFA transition table limit exceeded")

// ByteDFA is a DFA backed by a compact transition table. Bytes with identical
// transitions share a column. It can be shared across goroutines.
type ByteDFA struct {
	class  [256]uint8
	stride uint
	start  int
	final  []bool
	table  []uint32
}

// CompileByteDFA builds a table-backed DFA from a byte definition. maxTableBytes
// bounds the transition table alone, excluding accepting states and temporary
// compilation memory. Use CompileDFA for sparse automata or if the budget is exceeded.
func CompileByteDFA(def Definition[byte], maxTableBytes int) (*ByteDFA, error) {
	if maxTableBytes <= 0 {
		return nil, fmt.Errorf("%w: table budget must be positive", ErrInvalidDefinition)
	}
	if def.StateCount > 0 && uint64(def.StateCount) > uint64(^uint32(0)) {
		return nil, ErrTableLimit
	}
	// Every table has at least one column. Reject oversized definitions before
	// compiling the generic graph or doing any work proportional to StateCount.
	if def.StateCount > maxTableBytes/4 {
		return nil, ErrTableLimit
	}
	dfa, err := CompileDFA(def)
	if err != nil {
		return nil, err
	}

	// Encode the non-dead entries of each column in state order. Equal encoded
	// columns have identical behavior, including missing transitions.
	var columns [256][]byte
	for from := range dfa.rows {
		row := &dfa.rows[from]
		if row.target != 0 {
			columns[row.symbol] = appendByteEdge(columns[row.symbol], from, row.target-1)
		}
		for symbol, to := range row.extra {
			columns[symbol] = appendByteEdge(columns[symbol], from, to)
		}
	}
	result := &ByteDFA{start: dfa.start, final: dfa.final}
	ids := make(map[string]uint8)
	for symbol, column := range columns {
		key := string(column)
		id, exists := ids[key]
		if !exists {
			id = uint8(len(ids))
			ids[key] = id
		}
		result.class[symbol] = id
	}
	width := len(ids)
	if def.StateCount > maxTableBytes/4/width || uint64(def.StateCount)*uint64(width) > uint64(^uint32(0)) {
		return nil, ErrTableLimit
	}
	result.stride = uint(width)
	result.table = make([]uint32, def.StateCount*width)
	for from := range dfa.rows {
		row := &dfa.rows[from]
		base := from * width
		if row.target != 0 {
			result.table[base+int(result.class[row.symbol])] = uint32((row.target-1)*width + 1)
		}
		for symbol, to := range row.extra {
			result.table[base+int(result.class[symbol])] = uint32(to*width + 1)
		}
	}
	return result, nil
}

func appendByteEdge(column []byte, from, to int) []byte {
	column = binary.AppendUvarint(column, uint64(from))
	return binary.AppendUvarint(column, uint64(to))
}

// Start returns the initial state.
func (d *ByteDFA) Start() int { return d.start }

// IsAccepting reports whether state is an accepting state.
func (d *ByteDFA) IsAccepting(state int) bool {
	return state >= 0 && state < len(d.final) && d.final[state]
}

// Step returns the target of a single transition, if present.
func (d *ByteDFA) Step(state int, symbol byte) (int, bool) {
	if state < 0 || state >= len(d.final) {
		return 0, false
	}
	next := d.table[uint(state)*d.stride+uint(d.class[symbol])]
	if next == 0 {
		return 0, false
	}
	return int(uint(next-1) / d.stride), true
}

// Match reports whether the DFA accepts the entire input.
func (d *ByteDFA) Match(input []byte) bool {
	if len(d.final) == 0 {
		return false
	}
	row := uint(d.start) * d.stride
	for _, symbol := range input {
		target := d.table[row+uint(d.class[symbol])]
		if target == 0 {
			return false
		}
		row = uint(target - 1)
	}
	return d.final[row/d.stride]
}

// Analyze runs the DFA with optional observation.
func (d *ByteDFA) Analyze(input []byte, observer Observer) RunStats {
	return analyzeDFA(input, observer, len(d.final), d.start, d.Step, d.IsAccepting)
}
