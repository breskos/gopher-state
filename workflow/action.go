package workflow

import (
	"fmt"
	"io"
)

// Options lists a state's eligible edges in declaration order. It does not draw
// randomness. Unknown states and events return an empty list.
func (e *Engine) Options(current, event string, facts Facts) []Option {
	g := e.states[current][event]
	if g == nil {
		return nil
	}
	var options []Option
	for i := range g.edges {
		t := &g.edges[i]
		if t.chance != 0 && t.eligible(facts) {
			options = append(options, makeOption(Private, t.decision(current, event), nil, t, g.selection))
		}
	}
	return options
}

// Options lists eligible edges from the run's current state without advancing.
func (r *Run) Options(event string, facts Facts) []Option {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.world.Snapshot().Options(r.state, event, facts)
}

// StepTransition attempts an explicit edge with the supplied current facts.
// Its execution probability still applies. An invalid or no longer eligible
// choice leaves the run unchanged.
func (r *Run) StepTransition(event, transition string, facts Facts) (Decision, bool, error) {
	if transition == "" {
		return Decision{}, false, fmt.Errorf("%w: transition ID is required", ErrInvalidDefinition)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepOn(r.world.Snapshot(), event, transition, facts)
}

func (e *Engine) chooseExplicit(current, event, transition string, facts Facts, draw Draw) (*edge, Selection, error) {
	states := e.states[current]
	if states == nil {
		return nil, "", fmt.Errorf("%w: %q", ErrUnknownState, current)
	}
	g := states[event]
	if g == nil {
		return nil, "", nil
	}
	for i := range g.edges {
		t := &g.edges[i]
		if t.id != transition || !t.eligible(facts) {
			continue
		}
		ok, err := t.passesChance(draw)
		if err != nil || !ok {
			return nil, g.selection, err
		}
		return t, g.selection, nil
	}
	return nil, g.selection, nil
}

// Action is a processor's explicit transition choice and optional graph
// extension. The transition may be declared in the same patch.
type Action struct {
	Event      string `json:"event"`
	Transition string `json:"transition"`
	Facts      Facts  `json:"facts,omitempty"`
	Patch      Patch  `json:"patch,omitzero"`
}

// ApplyAction evaluates a processor's action against the latest world.
// The patch and run advancement take effect only if the chosen transition
// succeeds and its recorder accepts the record. Facts and application effects
// remain the caller's responsibility. Callbacks must not mutate this world or
// reenter the run during the action.
func (r *Run) ApplyAction(action Action) (Decision, bool, error) {
	if action.Transition == "" {
		return Decision{}, false, fmt.Errorf("%w: transition ID is required", ErrInvalidDefinition)
	}
	if len(action.Patch.States) == 0 && len(action.Patch.Transitions) == 0 {
		return r.StepTransition(action.Event, action.Transition, action.Facts)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.world.mu.Lock()
	defer r.world.mu.Unlock()
	next, err := extend(r.world.Snapshot(), action.Patch)
	if err != nil {
		return Decision{}, false, err
	}
	decision, ok, err := r.stepOn(next, action.Event, action.Transition, action.Facts)
	if err == nil && ok {
		r.world.engine.Store(next)
	}
	return decision, ok, err
}

// ApplyActionJSON reads one bounded action document and applies it.
func (r *Run) ApplyActionJSON(reader io.Reader, maxBytes int64) (Decision, bool, error) {
	var action Action
	if err := readDocument(reader, maxBytes, &action); err != nil {
		return Decision{}, false, err
	}
	return r.ApplyAction(action)
}
