package dfa

import (
	"encoding/hex"
	"errors"
	"log"
	"strings"
)

const (
	errStateNotExistent = "state not existent"
	directionDelimiter  = "->"
)

type Edge struct {
	From string
	To   string
}

type DFA struct {
	Name string
	// States holds the state name as well as the state structure
	States map[string]*State
	// StateLookup holds key: symbol->symbol and all the states that
	// are in between of this symbol->state->symbol constellation.
	StateLookup map[string][]string
	// EdgeLookup holds key: symbol and as value all state pairs that
	// are connected by this symbol.
	EdgeLookup map[string][]*Edge
	Indexed    bool
	Start      string
}

func NewDFA(name string) *DFA {
	return &DFA{
		Name: name,
	}
}

func (m *DFA) SetStart(state string) {
	m.Start = state
}

func (m *DFA) GetStart() string {
	return m.Start
}

func (m *DFA) SetState(state *State) {
	if m.States == nil {
		m.States = make(map[string]*State)
	}
	m.States[state.Name] = state
}

func (m *DFA) SetStates(states []*State) {
	if m.States == nil {
		m.States = make(map[string]*State)
	}
	for _, state := range states {
		m.States[state.Name] = state
	}
}

func (m *DFA) GetState(name string) (*State, bool) {
	if m.States[name] == nil {
		return nil, false
	}
	return m.States[name], true
}

func (m *DFA) StateExists(name string) bool {
	_, ok := m.GetState(name)
	return ok
}

// Step executes one step in the DFA and determines if this step
// is possible within this automaton.
func (m *DFA) Step(state, symbol string) (string, bool, error) {
	if m.States[state] == nil {
		return "", false, errors.New(errStateNotExistent)
	}
	if next, ok := m.States[state].Via(symbol); ok {
		return next, true, nil
	}
	return "", false, nil
}

func (m *DFA) buildKey(from, to string) string {
	data := []byte(strings.Join([]string{from, to}, directionDelimiter))
	return hex.EncodeToString(data[:])
}

// Index indexes all symbol to symbol transitions with the states
// that are in between. And also indexes a symbol with all the
// state pairs where it is in between.
func (m *DFA) Index() {
	m.StateLookup = make(map[string][]string)
	m.EdgeLookup = make(map[string][]*Edge)
	for _, state := range m.States {
		for symbol1, transition := range state.Transitions {
			// StateLookup
			for symbol2 := range m.States[transition].Transitions {
				hash := m.buildKey(symbol1, symbol2)
				if _, ok := m.StateLookup[hash]; !ok {
					m.StateLookup[hash] = make([]string, 0)
				}
				m.StateLookup[hash] = append(m.StateLookup[hash], transition)
			}
			// SymbolLookup
			if m.EdgeLookup[symbol1] == nil {
				m.EdgeLookup[symbol1] = make([]*Edge, 0)
			}
			m.EdgeLookup[symbol1] = append(m.EdgeLookup[symbol1], &Edge{
				From: state.Name,
				To:   transition,
			})
		}
	}
	m.Indexed = true
}

// InspectStates returns (if indexed) all states that have a connection
// from a symbol to a symbol. It answers the questions which state
// is in between these two symbols.
func (m *DFA) InspectStates(from, to string) []string {
	if !m.Indexed {
		m.Index()
		m.Indexed = true
	}
	if states, ok := m.StateLookup[m.buildKey(from, to)]; ok {
		return states
	}
	return []string{}
}

// InspectSymbols returns (if indexed) all states that have a connection
// from a symbol to a symbol. It answers the questions which state
// is in between these two symbols.
func (m *DFA) InspectSymbols(symbol string) []*Edge {
	if !m.Indexed {
		m.Index()
		m.Indexed = true
	}
	if states, ok := m.EdgeLookup[symbol]; ok {
		return states
	}
	return nil
}

// Run runs the DFA from the starting point with the given events
// and returns the states that the events have taken
func (m *DFA) Run(tokens []string) ([]string, bool) {
	var path []string
	if m.States == nil {
		log.Fatal("not able to run() DFA, no states")
	}
	if _, ok := m.States[m.Start]; !ok {
		log.Fatal("not able to run() DFA, no start state")
	}
	current := m.Start
	for _, token := range tokens {
		path = append(path, current)
		if m.States[current] == nil {
			log.Fatalf("state not existent")
		}

		if m.States[current].Final {
			return path, true
		}
		state, ok := m.States[current].Via(token)
		if !ok {
			return path, false
		}
		current = state
	}
	return path, true
}
