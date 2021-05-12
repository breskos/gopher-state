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

type DFA struct {
	Name string
	// States holds the state name as well as the state structure
	States map[string]*State
	// Lookup holds key: symbol->symbol and all the states that
	// are in between of this symbol->state->symbol constellation.
	Lookup  map[string][]string
	Indexed bool
	Start   string
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

func (m *DFA) Index() {
	m.Lookup = make(map[string][]string)
	for _, state := range m.States {
		for symbol1, transition := range state.Transitions {
			for symbol2 := range m.States[transition].Transitions {
				hash := m.buildKey(symbol1, symbol2)
				if _, ok := m.Lookup[hash]; !ok {
					m.Lookup[hash] = make([]string, 0)
				}
				m.Lookup[hash] = append(m.Lookup[hash], transition)
			}
		}
	}
	m.Indexed = true
}

// Inspect returns (if indexed) all states that have a connection
// from a symbol to a symbol. It answers the questions if a state
// is in between these two symbols.
func (m *DFA) Inspect(from, to string) []string {
	if !m.Indexed {
		m.Index()
		m.Indexed = true
	}
	if states, ok := m.Lookup[m.buildKey(from, to)]; ok {
		return states
	}
	return []string{}
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
