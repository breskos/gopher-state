package workflow

import (
	"fmt"
	"strings"
)

// Candidate describes an edge and its eligibility for the supplied facts.
type Candidate struct {
	Option
	Eligible bool      `json:"eligible"`
	Failures []Failure `json:"failures,omitempty"`
}

// Failure identifies a failed eligibility check and its location in the rule.
type Failure struct {
	Kind     string `json:"kind"`
	Field    string `json:"field,omitempty"`
	Op       string `json:"op,omitempty"`
	Expected Value  `json:"expected,omitzero"`
	Actual   Value  `json:"actual,omitzero"`
	Path     string `json:"path,omitempty"`
}

// Explain evaluates the state's candidates in the same order as Options.
// It includes rejected edges and neither draws randomness nor advances state.
func (e *Engine) Explain(current, event string, facts Facts) []Candidate {
	g := e.states[current][event]
	if g == nil {
		return nil
	}
	result := make([]Candidate, 0, len(g.edges))
	for i := range g.edges {
		t := &g.edges[i]
		option := makeOption(Private, t.decision(current, event), nil, t, g.selection)
		result = append(result, explainEdge(t, option, facts))
	}
	return result
}

// Explain evaluates the current run's candidates without advancing it.
func (r *Run) Explain(event string, facts Facts) []Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.world.Snapshot().Explain(r.state, event, facts)
}

// Explain evaluates private and visible shared candidates for a participant.
func (e *Encounter) Explain(r *Run, event string, facts Facts) []Candidate {
	if r == nil || r.world == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	private := r.world.Snapshot()
	result := private.Explain(r.state, event, facts)
	mapping := e.people[r]
	from := mapping.shared[r.state]
	if e.closed || from == "" {
		return result
	}
	g := e.world.Snapshot().states[from][event]
	if g == nil {
		return result
	}
	for i := range g.edges {
		t := &g.edges[i]
		if to := mapping.local[t.to]; to != "" {
			shared := t.decision(from, event)
			local := Decision{Transition: t.id, From: r.state, To: to, Event: event}
			candidate := *t
			candidate.retired = t.retired || private.retired[to]
			result = append(result, explainEdge(&candidate, makeOption(Shared, local, &shared, t, g.selection), facts))
		}
	}
	return result
}

func explainEdge(t *edge, option Option, facts Facts) Candidate {
	c := Candidate{Option: option}
	switch {
	case t.retired:
		c.Failures = []Failure{{Kind: "retired"}}
	case t.chance == 0:
		c.Failures = []Failure{{Kind: "probability", Actual: Number(0)}}
	case t.when == nil:
	case t.requirements == 0:
		c.Failures = t.when.failures(facts, "when", c.Failures)
	default:
		for i := range t.requirements {
			c.Failures = t.when.children[i].failures(facts, fmt.Sprintf("requires[%d]", i), c.Failures)
		}
		if len(t.when.children) > t.requirements {
			c.Failures = t.when.children[t.requirements].failures(facts, "when", c.Failures)
		}
	}
	c.Eligible = len(c.Failures) == 0
	return c
}

func (p *predicate) failures(facts Facts, path string, out []Failure) []Failure {
	if p.evaluate(facts) {
		return out
	}
	switch p.op {
	case opAll, opAny:
		name := "all"
		if p.op == opAny {
			name = "any"
		}
		for i := range p.children {
			out = p.children[i].failures(facts, fmt.Sprintf("%s.%s[%d]", path, name, i), out)
		}
		return out
	case opNot:
		return append(out, Failure{Kind: "not", Op: "not", Actual: Bool(true), Path: path + ".not"})
	}

	actual := facts[p.field]
	expected := p.value
	field := p.field
	if p.other != "" {
		expected = facts[p.other]
	}
	kind := "compare"
	switch {
	case actual.IsZero():
		kind = "missing"
	case p.other != "" && expected.IsZero():
		kind, field, actual = "missing", p.other, Value{}
	case !actual.valid() || (p.op != opExists && (!expected.valid() ||
		actual.kind != expected.kind || (p.op >= opLess && !actual.numeric()))):
		kind = "type"
	case actual.kind == quantity && actual.str != expected.str:
		kind = "unit"
	case strings.HasPrefix(path, "requires["):
		kind = "requirement"
	}
	operators := [...]string{"exists", "eq", "ne", "lt", "lte", "gt", "gte"}
	failure := Failure{Kind: kind, Field: field, Op: operators[p.op-opExists], Path: path}
	if actual.valid() {
		failure.Actual = actual
	}
	if expected.valid() {
		failure.Expected = expected
	}
	return append(out, failure)
}
