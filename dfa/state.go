package dfa

type State struct {
	// Name represents the name of the state
	Name string
	// Transitions represents the transitions of the state.
	// The map is structured map[Symbol]State
	Transitions map[string]string
	Final       bool
}

// NewState creates a new state
func NewState(name string) *State {
	return &State{
		Name:        name,
		Final:       false,
		Transitions: make(map[string]string),
	}
}

// GetTransitions returns the symbols that would lead to a transition
func (s *State) GetTransitions() map[string]string {
	return s.Transitions
}

// AddTransitions adds a bulk of symbols to the state that all end up in the same state
func (s *State) AddTransitions(state *State, symbols []string) {
	for _, symbol := range symbols {
		s.Transitions[symbol] = state.Name
	}
}

// AddTransitions adds a symbol that leads to a state - meaning a transition
func (s *State) AddTransition(state *State, symbol string) {
	if s.Transitions == nil {
		s.Transitions = make(map[string]string)
	}
	s.Transitions[symbol] = state.Name
}

// Via is used by the DFA to find a transition using a symbol
func (s *State) Via(symbol string) (string, bool) {
	for key, state := range s.Transitions {
		if key == symbol {
			return state, true
		}
	}
	return "", false
}

// IsFinal tests if this state is a final state
func (s *State) IsFinal() bool {
	return s.Final
}

// SetFinal sets this state to a final state
func (s *State) SetFinal(final bool) {
	s.Final = final
}
