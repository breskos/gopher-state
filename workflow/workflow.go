// Package workflow runs event-driven state transitions configured with JSON.
// A compiled Engine is immutable and can be shared by workflow instances.
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
)

// ErrInvalidDefinition identifies a malformed workflow definition.
var ErrInvalidDefinition = errors.New("invalid workflow definition")

// ErrUnknownState identifies a state outside the compiled graph.
var ErrUnknownState = errors.New("unknown workflow state")

// ErrInvalidDraw identifies a random draw outside its requested range.
var ErrInvalidDraw = errors.New("invalid workflow random draw")

// Selection determines how a group of transitions for one state and event
// chooses among eligible transitions.
type Selection string

const (
	// First selects the first eligible transition in declaration order.
	First Selection = "first"
	// Weighted samples one eligible transition by its relative weight.
	Weighted Selection = "weighted"
)

// Definition is the portable workflow model. Version must be 1.
type Definition struct {
	Version       int          `json:"version"`
	Start         string       `json:"start"`
	States        []string     `json:"states"`
	Transitions   []Transition `json:"transitions"`
	RetiredStates []string     `json:"retiredStates,omitempty"`
}

// Transition describes one possible move. Metadata is application-defined and
// does not affect eligibility or selection.
type Transition struct {
	Retired   bool          `json:"retired,omitempty"`
	ID        string        `json:"id"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Event     string        `json:"event"`
	Selection Selection     `json:"selection,omitempty"`
	When      *Condition    `json:"when,omitempty"`
	Requires  []Requirement `json:"requires,omitempty"`
	Weight    uint64        `json:"weight,omitempty"`
	// Probability gates the selected transition. Nil means certainty.
	Probability *float64                   `json:"probability,omitempty"`
	Metadata    map[string]json.RawMessage `json:"metadata,omitempty"`
}

// Requirement is a nonnegative minimum for a numeric or unit-bearing fact.
type Requirement struct {
	Field   string `json:"field"`
	AtLeast Value  `json:"atLeast"`
}

type edge struct {
	id, to       string
	when         *predicate
	weight       uint64
	chance       uint64
	retired      bool
	requirements int
}

const probabilityScale = 1 << 53

type group struct {
	selection Selection
	edges     []edge
	weight    uint64
}

// Engine executes a validated definition. Each call is independent.
type Engine struct {
	definition   Definition
	retired      map[string]bool
	origins      map[string]PatchInfo
	stateOrigins map[string]PatchInfo
	start        string
	states       map[string]map[string]*group
	metadata     map[string]map[string]json.RawMessage
}

// Compile validates a definition and indexes transitions by source and event.
func Compile(def Definition) (*Engine, error) {
	if def.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidDefinition, def.Version)
	}
	engine := &Engine{
		start: def.Start, states: make(map[string]map[string]*group, len(def.States)),
		metadata: make(map[string]map[string]json.RawMessage, len(def.Transitions)),
	}
	for _, state := range def.States {
		if _, exists := engine.states[state]; state == "" || exists {
			return nil, fmt.Errorf("%w: empty or duplicate state %q", ErrInvalidDefinition, state)
		}
		engine.states[state] = make(map[string]*group)
	}
	engine.retired = make(map[string]bool, len(def.RetiredStates))
	for _, state := range def.RetiredStates {
		if engine.states[state] == nil || engine.retired[state] {
			return nil, fmt.Errorf("%w: unknown or duplicate retired state %q", ErrInvalidDefinition, state)
		}
		engine.retired[state] = true
	}
	if engine.states[def.Start] == nil {
		return nil, fmt.Errorf("%w: unknown start state %q", ErrInvalidDefinition, def.Start)
	}
	for i, t := range def.Transitions {
		if err := engine.add(t); err != nil {
			return nil, fmt.Errorf("%w: transition %d (%q): %w", ErrInvalidDefinition, i, t.ID, err)
		}
	}
	engine.definition = cloneDefinition(def)
	return engine, nil
}

func (e *Engine) add(t Transition) error {
	if _, exists := e.metadata[t.ID]; t.ID == "" || exists {
		return fmt.Errorf("empty or duplicate transition ID")
	}
	events := e.states[t.From]
	if events == nil || e.states[t.To] == nil || t.Event == "" {
		return fmt.Errorf("requires known source and target states and a nonempty event")
	}
	selection := t.Selection
	if selection == "" {
		selection = First
	}
	g := events[t.Event]
	if g == nil {
		g = &group{selection: selection}
		events[t.Event] = g
	} else if g.selection != selection {
		return fmt.Errorf("mixed selection modes for the same source and event")
	}
	retired := t.Retired || e.retired[t.To]
	switch selection {
	case First:
		if t.Weight != 0 {
			return fmt.Errorf("weight is only valid with weighted selection")
		}
	case Weighted:
		if t.Weight == 0 || (!retired && t.Weight > ^uint64(0)-g.weight) {
			return fmt.Errorf("weights must be positive and their sum must fit in uint64")
		}
		if !retired {
			g.weight += t.Weight
		}
	default:
		return fmt.Errorf("unknown selection %q", selection)
	}
	compiled := edge{id: t.ID, to: t.To, weight: t.Weight, chance: probabilityScale, retired: retired}
	if t.Probability != nil {
		p := *t.Probability
		if !(p >= 0 && p <= 1) {
			return fmt.Errorf("probability must be between 0 and 1")
		}
		compiled.chance = uint64(p * probabilityScale)
	}
	when, err := compileEligibility(t)
	if err != nil {
		return err
	}
	compiled.when = when
	compiled.requirements = len(t.Requires)
	for name, value := range t.Metadata {
		if !json.Valid(value) {
			return fmt.Errorf("invalid JSON metadata at %q", name)
		}
	}
	e.metadata[t.ID] = cloneMetadata(t.Metadata)
	g.edges = append(g.edges, compiled)
	return nil
}

func compileEligibility(t Transition) (*predicate, error) {
	var when *predicate
	if t.When != nil {
		p, err := compileCondition(*t.When, 0)
		if err != nil {
			return nil, err
		}
		when = &p
	}
	if len(t.Requires) == 0 {
		return when, nil
	}
	children := make([]predicate, 0, len(t.Requires)+1)
	for i, req := range t.Requires {
		if req.Field == "" || !req.AtLeast.numeric() || !req.AtLeast.valid() || req.AtLeast.num < 0 {
			return nil, fmt.Errorf("requirement %d needs a field and a nonnegative numeric or quantity minimum", i)
		}
		children = append(children, predicate{field: req.Field, value: req.AtLeast, op: opGreaterEqual})
	}
	if when != nil {
		children = append(children, *when)
	}
	return &predicate{op: opAll, children: children}, nil
}

// Start returns the initial state of the workflow.
func (e *Engine) Start() string { return e.start }

// Metadata returns a deep copy of a transition's application-defined data.
// The boolean reports whether the transition exists.
func (e *Engine) Metadata(id string) (map[string]json.RawMessage, bool) {
	value, ok := e.metadata[id]
	return cloneMetadata(value), ok
}

func cloneMetadata(data map[string]json.RawMessage) map[string]json.RawMessage {
	if data == nil {
		return nil
	}
	copy := make(map[string]json.RawMessage, len(data))
	for key, value := range data {
		copy[key] = slices.Clone(value)
	}
	return copy
}

// Draw returns a random integer in [0, limit). Supply a seeded generator
// for reproducible runs. A nil Draw uses math/rand/v2.Uint64N.
type Draw func(limit uint64) uint64

// Decision describes the selected transition.
type Decision struct {
	Transition string `json:"transition"`
	From       string `json:"from"`
	To         string `json:"to"`
	Event      string `json:"event"`
}

// Step evaluates the event against the current facts. No eligible transition
// returns false and leaves the caller's state unchanged. First selection uses
// declaration order. Weighted selection draws only when multiple edges qualify.
// A selected transition can then be rejected by its probability, leaving the
// state unchanged without considering any later transitions.
func (e *Engine) Step(current, event string, facts Facts, draw Draw) (Decision, bool, error) {
	selected, _, err := e.choose(current, event, facts, draw)
	if err != nil || selected == nil {
		return Decision{}, false, err
	}
	return selected.decision(current, event), true, nil
}

func (t *edge) decision(from, event string) Decision {
	return Decision{Transition: t.id, From: from, To: t.to, Event: event}
}

func (e *Engine) choose(current, event string, facts Facts, draw Draw) (*edge, Selection, error) {
	events := e.states[current]
	if events == nil {
		return nil, "", fmt.Errorf("%w: %q", ErrUnknownState, current)
	}
	g := events[event]
	if g == nil {
		return nil, "", nil
	}
	selected, err := g.selectEdge(facts, draw)
	if err != nil || selected == nil {
		return nil, g.selection, err
	}
	if ok, err := selected.passesChance(draw); err != nil || !ok {
		return nil, g.selection, err
	}
	return selected, g.selection, nil
}

func (t *edge) passesChance(draw Draw) (bool, error) {
	switch t.chance {
	case 0:
		return false, nil
	case probabilityScale:
		return true, nil
	default:
		pick, err := drawWithin(draw, probabilityScale)
		return pick < t.chance, err
	}
}

func (t *edge) eligible(facts Facts) bool {
	return !t.retired && (t.when == nil || t.when.evaluate(facts))
}

func (g *group) selectEdge(facts Facts, draw Draw) (*edge, error) {
	var total uint64
	var selected *edge
	var count int
	for i := range g.edges {
		t := &g.edges[i]
		if !t.eligible(facts) {
			continue
		}
		if g.selection == First {
			return t, nil
		}
		total += t.weight
		count++
		selected = t
	}
	if count < 2 {
		return selected, nil
	}
	pick, err := drawWithin(draw, total)
	if err != nil {
		return nil, err
	}
	for i := range g.edges {
		t := &g.edges[i]
		if !t.eligible(facts) {
			continue
		}
		if pick < t.weight {
			return t, nil
		}
		pick -= t.weight
	}
	return nil, nil
}

func drawWithin(draw Draw, limit uint64) (uint64, error) {
	if draw == nil {
		draw = rand.Uint64N
	}
	pick := draw(limit)
	if pick >= limit {
		return 0, fmt.Errorf("%w: %d outside [0, %d)", ErrInvalidDraw, pick, limit)
	}
	return pick, nil
}

// Input is one workflow event and its fact snapshot.
type Input struct {
	Event string `json:"event"`
	Facts Facts  `json:"facts,omitempty"`
}

// RunResult records completed transitions and the state where execution ended.
// Consumed equals the number of completed transitions.
type RunResult struct {
	State    string
	Consumed int
	Path     []Decision
}

// Run starts at Start and processes inputs until a step cannot advance or an
// error occurs. The result includes transitions completed before stopping.
func (e *Engine) Run(inputs []Input, draw Draw) (RunResult, error) {
	result := RunResult{State: e.start}
	for _, input := range inputs {
		decision, ok, err := e.Step(result.State, input.Event, input.Facts, draw)
		if err != nil || !ok {
			return result, err
		}
		result.State = decision.To
		result.Consumed++
		result.Path = append(result.Path, decision)
	}
	return result, nil
}
