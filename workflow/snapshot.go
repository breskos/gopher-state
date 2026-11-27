package workflow

import (
	"errors"
	"fmt"
	"slices"
)

// ErrSequenceExhausted indicates that a run cannot assign another sequence number.
var ErrSequenceExhausted = errors.New("workflow run sequence exhausted")

// RunSnapshot holds the persistent identity and position of a run.
type RunSnapshot struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Sequence uint64 `json:"sequence"`
}

// Snapshot returns a consistent view of the run.
func (r *Run) Snapshot() RunSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RunSnapshot{ID: r.id, State: r.state, Sequence: r.sequence}
}

// ResumeRun restores a saved position. Randomness and recording are supplied
// by the caller, as with NewRun.
func (w *World) ResumeRun(s RunSnapshot, draw Draw, recorder Recorder) (*Run, error) {
	engine := w.Snapshot()
	if engine == nil || s.ID == "" {
		return nil, fmt.Errorf("%w: run requires an initialized world and a nonempty ID", ErrInvalidDefinition)
	}
	if engine.states[s.State] == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownState, s.State)
	}
	return &Run{world: w, id: s.ID, state: s.State, sequence: s.Sequence, draw: draw, recorder: recorder}, nil
}

// PatchInfo identifies who supplied an extension or retirement.
type PatchInfo struct {
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Retire identifies states and transitions to retire permanently.
type Retire struct {
	States      []string `json:"states,omitempty"`
	Transitions []string `json:"transitions,omitempty"`
}

func (p Patch) empty() bool {
	return len(p.States) == 0 && len(p.Transitions) == 0 &&
		len(p.Retire.States) == 0 && len(p.Retire.Transitions) == 0
}

// Definition returns an independent copy in insertion order.
func (e *Engine) Definition() Definition { return cloneDefinition(e.definition) }

func cloneDefinition(d Definition) Definition {
	d.States = slices.Clone(d.States)
	d.Transitions = cloneTransitions(d.Transitions)
	d.RetiredStates = slices.Clone(d.RetiredStates)
	return d
}

func cloneTransitions(ts []Transition) []Transition {
	result := slices.Clone(ts)
	for i := range result {
		t := &result[i]
		if t.When != nil {
			c := cloneCondition(*t.When)
			t.When = &c
		}
		if t.Probability != nil {
			p := *t.Probability
			t.Probability = &p
		}
		t.Requires = slices.Clone(t.Requires)
		t.Metadata = cloneMetadata(t.Metadata)
	}
	return result
}

func clonePatch(p Patch) Patch {
	p.States = slices.Clone(p.States)
	p.Transitions = cloneTransitions(p.Transitions)
	p.Retire.States = slices.Clone(p.Retire.States)
	p.Retire.Transitions = slices.Clone(p.Retire.Transitions)
	return p
}

func (w *World) preparePatch(p Patch) (*Engine, error) {
	if _, exists := w.patchIDs[p.Info.ID]; p.Info.ID != "" && exists {
		return nil, fmt.Errorf("%w: duplicate patch ID %q", ErrInvalidDefinition, p.Info.ID)
	}
	return extend(w.Snapshot(), p)
}

func (w *World) publishPatch(next *Engine, p Patch) {
	if !p.empty() || p.Info != (PatchInfo{}) {
		w.patches = append(w.patches, clonePatch(p))
		if p.Info.ID != "" {
			w.patchIDs[p.Info.ID] = struct{}{}
		}
	}
	w.engine.Store(next)
}

// Patches returns independent copies of accepted patches in publication order.
func (w *World) Patches() []Patch {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := slices.Clone(w.patches)
	for i := range result {
		result[i] = clonePatch(result[i])
	}
	return result
}

// RestoreWorld recreates a world from its original base and accepted patches.
func RestoreWorld(base *Engine, patches []Patch) (*World, error) {
	w, err := NewWorld(base)
	if err != nil {
		return nil, err
	}
	for i, patch := range patches {
		if err := w.Add(patch); err != nil {
			return nil, fmt.Errorf("patch %d (%q): %w", i, patch.Info.ID, err)
		}
	}
	return w, nil
}

// TransitionOrigin reports the provenance of an edge added by an identified patch.
func (e *Engine) TransitionOrigin(id string) (PatchInfo, bool) {
	info, ok := e.origins[id]
	return info, ok
}

func retire(def Definition, retirement Retire) (*Engine, error) {
	states := make(map[string]bool, len(def.States))
	for _, state := range def.States {
		states[state] = true
	}
	for _, state := range retirement.States {
		if !states[state] || slices.Contains(def.RetiredStates, state) {
			return nil, fmt.Errorf("%w: unknown or already retired state %q", ErrInvalidDefinition, state)
		}
		def.RetiredStates = append(def.RetiredStates, state)
	}
	index := make(map[string]int, len(def.Transitions))
	for i, t := range def.Transitions {
		index[t.ID] = i
	}
	for _, id := range retirement.Transitions {
		i, ok := index[id]
		if !ok || def.Transitions[i].Retired {
			return nil, fmt.Errorf("%w: unknown or already retired transition %q", ErrInvalidDefinition, id)
		}
		def.Transitions[i].Retired = true
	}
	return Compile(def)
}
