package workflow

import "fmt"

// Condition is one comparison or one boolean expression. Use exactly one of
// All, Any, Not, or the Field and Op form. The nesting limit is 64.
type Condition struct {
	All        []Condition `json:"all,omitempty"`
	Any        []Condition `json:"any,omitempty"`
	Not        *Condition  `json:"not,omitempty"`
	Field      string      `json:"field,omitempty"`
	Op         string      `json:"op,omitempty"`
	Value      Value       `json:"value,omitzero"`
	OtherField string      `json:"otherField,omitempty"`
}

type operation uint8

const (
	opAll operation = iota
	opAny
	opNot
	opExists
	opEqual
	opNotEqual
	opLess
	opLessEqual
	opGreater
	opGreaterEqual
)

type predicate struct {
	field    string
	other    string
	value    Value
	children []predicate
	op       operation
}

func compileCondition(c Condition, depth int) (predicate, error) {
	if depth >= 64 {
		return predicate{}, fmt.Errorf("condition nesting exceeds 64 levels")
	}
	comparison := c.Field != "" || c.Op != "" || !c.Value.IsZero() || c.OtherField != ""
	forms := 0
	for _, present := range []bool{c.All != nil, c.Any != nil, c.Not != nil, comparison} {
		if present {
			forms++
		}
	}
	if forms != 1 {
		return predicate{}, fmt.Errorf("condition must use exactly one form")
	}

	switch {
	case c.All != nil:
		return compileGroup(opAll, c.All, depth)
	case c.Any != nil:
		return compileGroup(opAny, c.Any, depth)
	case c.Not != nil:
		return compileGroup(opNot, []Condition{*c.Not}, depth)
	}

	if c.Field == "" {
		return predicate{}, fmt.Errorf("comparison requires a field")
	}
	p := predicate{field: c.Field, value: c.Value, other: c.OtherField}
	switch c.Op {
	case "exists":
		if !c.Value.IsZero() || c.OtherField != "" {
			return predicate{}, fmt.Errorf("exists takes no value or otherField")
		}
		p.op = opExists
		return p, nil
	case "eq":
		p.op = opEqual
	case "ne":
		p.op = opNotEqual
	case "lt":
		p.op = opLess
	case "lte":
		p.op = opLessEqual
	case "gt":
		p.op = opGreater
	case "gte":
		p.op = opGreaterEqual
	default:
		return predicate{}, fmt.Errorf("unsupported condition operator %q", c.Op)
	}
	if c.OtherField != "" {
		if !c.Value.IsZero() {
			return predicate{}, fmt.Errorf("%s takes either value or otherField", c.Op)
		}
		return p, nil
	}
	if !c.Value.valid() {
		return predicate{}, fmt.Errorf("%s requires a finite scalar or quantity", c.Op)
	}
	if p.op >= opLess && !c.Value.numeric() {
		return predicate{}, fmt.Errorf("%s requires a numeric value or quantity", c.Op)
	}
	return p, nil
}

func compileGroup(op operation, children []Condition, depth int) (predicate, error) {
	if len(children) == 0 {
		return predicate{}, fmt.Errorf("boolean group requires at least one operand")
	}
	result := predicate{op: op, children: make([]predicate, len(children))}
	for i, child := range children {
		compiled, err := compileCondition(child, depth+1)
		if err != nil {
			return predicate{}, fmt.Errorf("operand %d: %w", i, err)
		}
		result.children[i] = compiled
	}
	return result, nil
}

func (p *predicate) evaluate(facts Facts) bool {
	switch p.op {
	case opAll:
		for i := range p.children {
			if !p.children[i].evaluate(facts) {
				return false
			}
		}
		return true
	case opAny:
		for i := range p.children {
			if p.children[i].evaluate(facts) {
				return true
			}
		}
		return false
	case opNot:
		return !p.children[0].evaluate(facts)
	}

	actual := facts[p.field]
	if !actual.valid() {
		return false
	}
	if p.op == opExists {
		return true
	}
	expected := p.value
	if p.other != "" {
		expected = facts[p.other]
		if !expected.valid() || (p.op >= opLess && !expected.numeric()) {
			return false
		}
	}
	if actual.kind != expected.kind || (actual.kind == quantity && actual.str != expected.str) {
		return false
	}
	switch p.op {
	case opEqual:
		return actual == expected
	case opNotEqual:
		return actual != expected
	case opLess:
		return actual.num < expected.num
	case opLessEqual:
		return actual.num <= expected.num
	case opGreater:
		return actual.num > expected.num
	default:
		return actual.num >= expected.num
	}
}
