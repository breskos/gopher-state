// Package dfa provides the original mutable, string-based DFA API.
// For validated, immutable automata use the module's root package.
package dfa

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	errStateNotExistent = "state not existent"
	directionDelimiter  = "->"
)

// Edge represents a connection from a state to a state
type Edge struct {
	From string
	To   string
}

// DFA holds everything that is needed in order to execute the automaton.
type DFA struct {
	Name string
	// States holds the state name as well as the state structure
	States map[string]*State
	// StateLookup is a legacy snapshot populated by Index.
	StateLookup map[string][]string
	// EdgeLookup is a legacy snapshot populated by Index.
	EdgeLookup map[string][]*Edge
	// Indexed reports whether a snapshot has been built since the last SetState.
	Indexed bool
	Start   string
}

// NewDFA creates a new DFA
func NewDFA(name string) *DFA {
	return &DFA{
		Name:    name,
		Indexed: false,
	}
}

// SetStart sets the starting point of the DFA.
func (m *DFA) SetStart(state string) {
	m.Start = state
}

// GetStart returns the starting point of the DFA.
func (m *DFA) GetStart() string {
	return m.Start
}

// SetState sets one state
func (m *DFA) SetState(state *State) {
	if m.States == nil {
		m.States = make(map[string]*State)
	}
	m.States[state.Name] = state
	m.Indexed = false
}

// SetStates is able to set multiple states at once
func (m *DFA) SetStates(states []*State) {
	for _, state := range states {
		m.SetState(state)
	}
}

// GetState returns the specific state with a given name
func (m *DFA) GetState(name string) *State {
	if m.StateExists(name) {
		return m.States[name]
	}
	return nil
}

// StateExists tests if the state exists
func (m *DFA) StateExists(name string) bool {
	if m.States[name] == nil {
		return false
	}
	return true
}

// Step executes one step in the DFA and determines if this step
// is possible within this automaton.
func (m *DFA) Step(state, symbol string) (string, bool, error) {
	if m.States[state] == nil {
		return "", false, errors.New(errStateNotExistent)
	}
	if next, ok := m.States[state].Via(symbol); ok {
		if !m.StateExists(next) {
			return "", false, fmt.Errorf("%s: %q", errStateNotExistent, next)
		}
		return next, true, nil
	}
	return "", false, nil
}

func (m *DFA) buildKey(from, to string) string {
	if strings.Contains(from, directionDelimiter) || strings.Contains(to, directionDelimiter) {
		return ":" + strconv.Quote(from) + ":" + strconv.Quote(to)
	}
	data := []byte(from + directionDelimiter + to)
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// Index indexes all symbol to symbol transitions with the states
// that are in between. And also indexes a symbol with all the
// state pairs where it is in between.
func (m *DFA) Index() {
	m.StateLookup = make(map[string][]string)
	m.EdgeLookup = make(map[string][]*Edge)
	for _, state := range m.States {
		if state == nil {
			continue
		}
		for symbol1, transition := range state.Transitions {
			// StateLookup
			target := m.States[transition]
			if target == nil {
				continue
			}
			for symbol2, end := range target.Transitions {
				if !m.StateExists(end) {
					continue
				}
				hash := m.buildKey(symbol1, symbol2)
				m.StateLookup[hash] = append(m.StateLookup[hash], transition)
			}
			// SymbolLookup
			m.EdgeLookup[symbol1] = append(m.EdgeLookup[symbol1], &Edge{
				From: state.Name,
				To:   transition,
			})
		}
	}
	m.Indexed = true
}

// InspectStates returns the intermediate state names for paths with the
// given pair of symbols. Each source state with such a path contributes an entry.
func (m *DFA) InspectStates(from, to string) []string {
	states := []string{}
	for _, state := range m.States {
		if state == nil {
			continue
		}
		next, ok := state.Via(from)
		if !ok || !m.StateExists(next) {
			continue
		}
		target, ok := m.States[next].Via(to)
		if ok && m.StateExists(target) {
			states = append(states, next)
		}
	}
	sort.Strings(states)
	return states
}

// InspectSymbols returns the edges labeled with symbol.
func (m *DFA) InspectSymbols(symbol string) []*Edge {
	var edges []*Edge
	for _, state := range m.States {
		if state == nil {
			continue
		}
		target, ok := state.Via(symbol)
		if ok && m.StateExists(target) {
			edges = append(edges, &Edge{From: state.Name, To: target})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	return edges
}

// GetSymbols returns the distinct symbols with valid transitions in sorted order.
func (m *DFA) GetSymbols() []string {
	unique := make(map[string]struct{})
	for _, state := range m.States {
		if state == nil {
			continue
		}
		for symbol, target := range state.Transitions {
			if m.StateExists(target) {
				unique[symbol] = struct{}{}
			}
		}
	}
	var symbols []string
	for symbol := range unique {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

// Run returns the visited path and whether the entire input was accepted.
// Invalid configurations yield false. Use RunChecked for error details.
func (m *DFA) Run(tokens []string) ([]string, bool) {
	path, accepted, _ := m.RunChecked(tokens)
	return path, accepted
}

// RunChecked returns the path, including the start state and each state
// reached by a successful step, and whether the entire input was accepted.
// A missing transition rejects the input. An invalid start or a traversed
// transition to an unregistered state returns an error.
func (m *DFA) RunChecked(tokens []string) ([]string, bool, error) {
	current := m.Start
	if !m.StateExists(current) {
		return nil, false, fmt.Errorf("invalid start state %q", current)
	}
	path := []string{current}
	for _, token := range tokens {
		next, ok, err := m.Step(current, token)
		if !ok {
			return path, false, err
		}
		path = append(path, next)
		current = next
	}
	return path, m.States[current].Final, nil
}
