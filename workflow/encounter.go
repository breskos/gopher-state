package workflow

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Scope identifies whether an option belongs to a private or shared graph.
type Scope string

const (
	Private Scope = "private"
	Shared  Scope = "shared"
)

// Binding maps shared state IDs to local state IDs for one person.
// Each local state may map to at most one shared state in an encounter.
type Binding struct {
	Run    *Run
	States map[string]string
}

type meetingStates struct {
	local  map[string]string // Shared to local.
	shared map[string]string // Local to shared.
}

// Encounter makes the supplied world's edges visible to explicitly bound
// participants while they are in mapped local states. Close ends visibility.
// Do not copy an Encounter after use.
type Encounter struct {
	mu     sync.RWMutex
	id     string
	world  *World
	people map[*Run]meetingStates
	closed bool
}

// NewEncounter opens an encounter. A participant's binding is copied and
// validated against the current private and shared graphs.
func NewEncounter(id string, shared *World, bindings []Binding) (*Encounter, error) {
	if id == "" || shared == nil || shared.Snapshot() == nil || len(bindings) == 0 {
		return nil, fmt.Errorf("%w: encounter requires an ID, shared world and participants", ErrInvalidDefinition)
	}
	public := shared.Snapshot()
	e := &Encounter{id: id, world: shared, people: make(map[*Run]meetingStates, len(bindings))}
	ids := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		if b.Run == nil || b.Run.world == nil || ids[b.Run.id] || len(b.States) == 0 {
			return nil, fmt.Errorf("%w: encounter requires distinct runs with nonempty bindings", ErrInvalidDefinition)
		}
		ids[b.Run.id] = true
		private := b.Run.world.Snapshot()
		mapping := meetingStates{local: maps.Clone(b.States), shared: make(map[string]string, len(b.States))}
		for from, to := range b.States {
			if public.states[from] == nil || private.states[to] == nil || mapping.shared[to] != "" {
				return nil, fmt.Errorf("%w: invalid binding %q to %q for run %q", ErrInvalidDefinition, from, to, b.Run.id)
			}
			mapping.shared[to] = from
		}
		e.people[b.Run] = mapping
	}
	return e, nil
}

// Close disables shared options and waits for active encounter actions.
// Private moves are still available.
func (e *Encounter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
}

// Option is a currently eligible edge. Decision uses the person's local
// states. SharedDecision identifies the common edge for shared options.
type Option struct {
	Scope          Scope
	Decision       Decision
	SharedDecision *Decision
	Selection      Selection
	Weight         uint64
	Probability    float64
}

// Options lists a person's eligible private and shared choices for one event.
// It does not draw randomness or advance any run.
func (e *Encounter) Options(r *Run, event string, facts Facts) []Option {
	if r == nil || r.world == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	private := r.world.Snapshot()
	options := private.Options(r.state, event, facts)
	mapping := e.people[r]
	from := mapping.shared[r.state]
	if e.closed || from == "" {
		return options
	}
	public := e.world.Snapshot().states[from][event]
	if public != nil {
		for i := range public.edges {
			t := &public.edges[i]
			if to := mapping.local[t.to]; to != "" && !private.retired[to] && t.chance != 0 && t.eligible(facts) {
				shared := t.decision(from, event)
				local := Decision{Transition: t.id, From: r.state, To: to, Event: event}
				options = append(options, makeOption(Shared, local, &shared, t, public.selection))
			}
		}
	}
	return options
}

func makeOption(scope Scope, decision Decision, shared *Decision, t *edge, selection Selection) Option {
	return Option{
		Scope: scope, Decision: decision, SharedDecision: shared, Selection: selection,
		Weight: t.weight, Probability: float64(t.chance) / probabilityScale,
	}
}

// Step takes a private or shared edge individually. An empty transition ID
// uses the graph's selection policy. An explicit eligible ID skips relative
// selection and still applies its execution probability.
func (e *Encounter) Step(r *Run, scope Scope, event, transition string, facts Facts) (Decision, bool, error) {
	return e.StepWith(r, scope, event, transition, facts, StepOptions{})
}

// StepWith executes one private or shared move with per-step options.
func (e *Encounter) StepWith(r *Run, scope Scope, event, transition string, facts Facts, opt StepOptions) (Decision, bool, error) {
	if r == nil || r.world == nil {
		return Decision{}, false, fmt.Errorf("%w: initialized run required", ErrInvalidDefinition)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	switch scope {
	case Private:
		return r.stepOn(r.world.Snapshot(), event, transition, facts, opt)
	case Shared:
		mapping := e.people[r]
		from := mapping.shared[r.state]
		if e.closed || from == "" {
			return Decision{}, false, nil
		}
		g := e.world.Snapshot().states[from][event]
		private := r.world.Snapshot()
		selected, err := chooseMeetingEdge(g, transition, opt.draw(r.draw), func(t *edge) bool {
			to := mapping.local[t.to]
			return to != "" && !private.retired[to] && t.eligible(facts)
		})
		if err != nil || selected == nil {
			return Decision{}, false, err
		}
		shared := selected.decision(from, event)
		local := Decision{Transition: selected.id, From: r.state, To: mapping.local[selected.to], Event: event}
		return r.advance(selected, g.selection, local, facts, e.id, &shared, opt)
	default:
		return Decision{}, false, fmt.Errorf("%w: unknown scope %q", ErrInvalidDefinition, scope)
	}
}

// Turn supplies one person's consent to a joint action and their current facts.
type Turn struct {
	Run   *Run
	Facts Facts
}

// Together selects a shared edge that all supplied people can take. They must
// share a mapped position. Selection and chance are drawn once for the group.
// Results are returned in the supplied order. If any selected run has a
// recorder, supply a group recorder to persist those records together.
func (e *Encounter) Together(event, transition string, people []Turn, draw Draw, recorder GroupRecorder) ([]Decision, bool, error) {
	return e.TogetherWith(event, transition, people, StepOptions{Draw: draw}, recorder)
}

// TogetherWith executes a joint move with one group draw source and metadata
// copied into each participant's record.
func (e *Encounter) TogetherWith(event, transition string, people []Turn, opt StepOptions, recorder GroupRecorder) ([]Decision, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, false, nil
	}
	if len(people) < 2 {
		return nil, false, fmt.Errorf("%w: joint action requires at least two participants", ErrInvalidDefinition)
	}
	ordered := make([]*Run, 0, len(people))
	for _, turn := range people {
		if _, member := e.people[turn.Run]; !member {
			return nil, false, fmt.Errorf("%w: joint action requires enrolled participants", ErrInvalidDefinition)
		}
		ordered = append(ordered, turn.Run)
	}
	slices.SortFunc(ordered, func(a, b *Run) int { return strings.Compare(a.id, b.id) })
	for i := 1; i < len(ordered); i++ {
		if ordered[i] == ordered[i-1] {
			return nil, false, fmt.Errorf("%w: duplicate participant in joint action", ErrInvalidDefinition)
		}
	}

	// Immutable IDs establish the same lock order across encounters.
	for _, r := range ordered {
		r.mu.Lock()
	}
	defer func() {
		for _, r := range ordered {
			r.mu.Unlock()
		}
	}()
	from := e.people[ordered[0]].shared[ordered[0].state]
	if from == "" {
		return nil, false, nil
	}
	logging := false
	for _, r := range ordered {
		if e.people[r].shared[r.state] != from {
			return nil, false, nil
		}
		logging = logging || r.recorder != nil
	}
	if logging && recorder == nil {
		return nil, false, fmt.Errorf("%w: joint logging requires a group recorder", ErrInvalidDefinition)
	}
	private := make([]*Engine, len(people))
	for i, turn := range people {
		private[i] = turn.Run.world.Snapshot()
	}
	g := e.world.Snapshot().states[from][event]
	selected, err := chooseMeetingEdge(g, transition, opt.Draw, func(t *edge) bool {
		for i, turn := range people {
			to := e.people[turn.Run].local[t.to]
			if to == "" || private[i].retired[to] || !t.eligible(turn.Facts) {
				return false
			}
		}
		return true
	})
	if err != nil || selected == nil {
		return nil, false, err
	}

	shared := selected.decision(from, event)
	results := make([]Decision, len(people))
	var records []Record
	for i, turn := range people {
		r := turn.Run
		local := Decision{Transition: selected.id, From: r.state, To: e.people[r].local[selected.to], Event: event}
		results[i] = local
		if r.recorder != nil {
			records = append(records, r.record(selected, g.selection, local, turn.Facts, e.id, &shared, opt))
		}
	}
	if len(records) != 0 {
		if err := recorder.WriteGroup(records); err != nil {
			return nil, false, fmt.Errorf("record joint action %q: %w", selected.id, err)
		}
	}
	for i, turn := range people {
		turn.Run.state = results[i].To
		turn.Run.sequence++
	}
	return results, true, nil
}

func chooseMeetingEdge(g *group, id string, draw Draw, eligible func(*edge) bool) (*edge, error) {
	if g == nil {
		return nil, nil
	}
	var total uint64
	var selected *edge
	var candidates []*edge
	for i := range g.edges {
		t := &g.edges[i]
		if (id != "" && t.id != id) || !eligible(t) {
			continue
		}
		selected = t
		if id != "" || g.selection == First {
			break
		}
		total += t.weight
		candidates = append(candidates, t)
	}
	if len(candidates) > 1 {
		pick, err := drawWithin(draw, total)
		if err != nil {
			return nil, err
		}
		for _, t := range candidates {
			if pick < t.weight {
				selected = t
				break
			}
			pick -= t.weight
		}
	}
	if selected == nil {
		return nil, nil
	}
	if ok, err := selected.passesChance(draw); err != nil || !ok {
		return nil, err
	}
	return selected, nil
}
