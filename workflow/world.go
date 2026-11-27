package workflow

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
)

// Patch adds states and transitions to a world. Targets may refer to existing
// states or to states in the same patch.
type Patch struct {
	States      []string     `json:"states,omitempty"`
	Transitions []Transition `json:"transitions,omitempty"`
}

// World holds a live, append-only workflow graph shared by independent runs.
// Each step uses one immutable snapshot. Do not copy a World.
type World struct {
	mu     sync.Mutex
	engine atomic.Pointer[Engine]
}

// NewWorld creates a world from a compiled engine. Patches to the world leave
// the original engine and other worlds unchanged.
func NewWorld(engine *Engine) (*World, error) {
	if engine == nil || engine.states[engine.start] == nil {
		return nil, fmt.Errorf("%w: world requires a compiled engine", ErrInvalidDefinition)
	}
	w := &World{}
	w.engine.Store(engine)
	return w, nil
}

// Snapshot returns the immutable engine currently used by the world.
func (w *World) Snapshot() *Engine { return w.engine.Load() }

// AddJSON reads a bounded JSON patch with the same transition format as
// ReadJSON, then applies it atomically.
func (w *World) AddJSON(r io.Reader, maxBytes int64) error {
	var patch *Patch
	if err := readDocument(r, maxBytes, &patch); err != nil {
		return err
	}
	if patch == nil {
		return fmt.Errorf("%w: patch must be an object", ErrInvalidDefinition)
	}
	return w.Add(*patch)
}

// Add publishes an extension after all new states and transitions validate.
// Declaration order appends to existing groups. An invalid patch has no effect.
func (w *World) Add(patch Patch) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	next, err := extend(w.Snapshot(), patch)
	if err != nil {
		return err
	}
	w.engine.Store(next)
	return nil
}

func extend(old *Engine, patch Patch) (*Engine, error) {
	if old == nil {
		return nil, fmt.Errorf("%w: uninitialized world", ErrInvalidDefinition)
	}
	if len(patch.States) == 0 && len(patch.Transitions) == 0 {
		return old, nil
	}
	next := &Engine{start: old.start, states: maps.Clone(old.states), metadata: maps.Clone(old.metadata)}
	changedStates := make(map[string]bool)
	for _, state := range patch.States {
		if state == "" || next.states[state] != nil {
			return nil, fmt.Errorf("%w: empty or duplicate state %q", ErrInvalidDefinition, state)
		}
		next.states[state] = make(map[string]*group)
		changedStates[state] = true
	}
	type eventKey struct{ state, event string }
	changedGroups := make(map[eventKey]bool)
	for i, t := range patch.Transitions {
		if events := next.states[t.From]; events != nil {
			if !changedStates[t.From] {
				next.states[t.From] = maps.Clone(events)
				changedStates[t.From] = true
			}
			key := eventKey{t.From, t.Event}
			if previous := next.states[t.From][t.Event]; previous != nil && !changedGroups[key] {
				next.states[t.From][t.Event] = &group{
					selection: previous.selection, edges: slices.Clone(previous.edges), weight: previous.weight,
				}
			}
			changedGroups[key] = true
		}
		if err := next.add(t); err != nil {
			return nil, fmt.Errorf("%w: patch transition %d (%q): %w", ErrInvalidDefinition, i, t.ID, err)
		}
	}
	return next, nil
}

// Run tracks one person's state and records successful transitions if a
// recorder was supplied. Steps on a single Run are serialized. Do not copy it.
type Run struct {
	mu       sync.Mutex
	world    *World
	id       string
	state    string
	sequence uint64
	draw     Draw
	recorder Recorder
}

// NewRun starts a person at the world's start state. Supply a unique ID when
// sharing a recorder and an independent Draw when using seeded randomness.
func (w *World) NewRun(id string, draw Draw, recorder Recorder) (*Run, error) {
	engine := w.Snapshot()
	if engine == nil || id == "" {
		return nil, fmt.Errorf("%w: run requires an initialized world and a nonempty ID", ErrInvalidDefinition)
	}
	return &Run{world: w, id: id, state: engine.start, draw: draw, recorder: recorder}, nil
}

// State returns the person's current state.
func (r *Run) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Step processes one event. When a recorder is configured, its WriteRecord
// must succeed before the state advances. A recorder should not call methods
// on the same Run. Use the returned Decision to apply application effects.
func (r *Run) Step(event string, facts Facts) (Decision, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.stepOn(r.world.Snapshot(), event, "", facts)
}

func (r *Run) stepOn(engine *Engine, event, transition string, facts Facts) (Decision, bool, error) {
	var selected *edge
	var selection Selection
	var err error
	if transition == "" {
		selected, selection, err = engine.choose(r.state, event, facts, r.draw)
	} else {
		selected, selection, err = engine.chooseExplicit(r.state, event, transition, facts, r.draw)
	}
	if err != nil || selected == nil {
		return Decision{}, false, err
	}
	return r.advance(selected, selection, selected.decision(r.state, event), facts, "", nil)
}

func (r *Run) advance(selected *edge, selection Selection, decision Decision, facts Facts, encounter string, shared *Decision) (Decision, bool, error) {
	if r.recorder != nil {
		record := r.record(selected, selection, decision, facts, encounter, shared)
		if err := r.recorder.WriteRecord(record); err != nil {
			return Decision{}, false, fmt.Errorf("record run %q transition %d: %w", r.id, record.Sequence, err)
		}
	}
	r.state = decision.To
	r.sequence++
	return decision, true, nil
}

func (r *Run) record(selected *edge, selection Selection, decision Decision, facts Facts, encounter string, shared *Decision) Record {
	record := Record{
		RunID: r.id, Sequence: r.sequence + 1, Decision: decision,
		Selection: selection, Weight: selected.weight,
		Probability: float64(selected.chance) / probabilityScale,
		Encounter:   encounter, Shared: shared,
	}
	if selected.when != nil {
		record.Facts = make(Facts)
		condition := selected.when.describe(facts, record.Facts)
		record.Condition = &condition
	}
	return record
}
