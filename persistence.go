package gopherstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"unicode/utf8"
)

// JSONSymbol is a scalar with a portable representation in the JSON format.
type JSONSymbol interface {
	~string | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// ErrInvalidJSON identifies an unsupported or malformed JSON automaton document.
var ErrInvalidJSON = errors.New("invalid automaton JSON")

// Kind identifies the execution semantics of a persisted definition.
type Kind string

const (
	KindDFA Kind = "dfa"
	KindNFA Kind = "nfa"
)

// JSONDefinition is the portable definition persisted by ReadJSON and WriteJSON.
type JSONDefinition[S JSONSymbol] struct {
	Kind Kind
	Definition[S]
	Epsilon []EpsilonTransition
}

// Machine is the shared matching and observation API for DFA and NFA.
type Machine[S comparable] interface {
	Match([]S) bool
	Analyze([]S, Observer) RunStats
}

// Compile validates the definition and builds an immutable machine.
func (d JSONDefinition[S]) Compile() (Machine[S], error) {
	switch d.Kind {
	case KindDFA:
		if len(d.Epsilon) != 0 {
			return nil, fmt.Errorf("%w: DFA cannot have epsilon transitions", ErrInvalidDefinition)
		}
		machine, err := CompileDFA(d.Definition)
		if err != nil {
			return nil, err
		}
		return machine, nil
	case KindNFA:
		machine, err := CompileNFA(NFADefinition[S]{Definition: d.Definition, Epsilon: d.Epsilon})
		if err != nil {
			return nil, err
		}
		return machine, nil
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidDefinition, d.Kind)
	}
}

// JSONLimits bounds document bytes and the declared number of states. Both
// limits must be positive. Choose limits before reading untrusted documents.
type JSONLimits struct {
	MaxBytes  int64
	MaxStates int
}

type jsonDocument[S JSONSymbol] struct {
	Version     int                 `json:"version"`
	Kind        Kind                `json:"kind"`
	StateCount  int                 `json:"stateCount"`
	Start       *int                `json:"start"`
	Accepting   []*int              `json:"accepting,omitempty"`
	Transitions []jsonTransition[S] `json:"transitions,omitempty"`
	Epsilon     []jsonEdge          `json:"epsilon,omitempty"`
}

type jsonEdge struct {
	From *int `json:"from"`
	To   *int `json:"to"`
}

type jsonTransition[S JSONSymbol] struct {
	jsonEdge
	Symbol *S `json:"symbol"`
}

// ReadJSON reads one version-1 document. It validates the wire format and
// limits. Call Compile to validate the graph and prepare it for execution.
func ReadJSON[S JSONSymbol](r io.Reader, limits JSONLimits) (JSONDefinition[S], error) {
	var result JSONDefinition[S]
	if limits.MaxBytes <= 0 || limits.MaxStates <= 0 {
		return result, fmt.Errorf("%w: limits must be positive", ErrInvalidJSON)
	}
	limited := &io.LimitedReader{R: r, N: limits.MaxBytes}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return result, fmt.Errorf("read automaton: %w", err)
	}
	if limited.N == 0 {
		var extra [1]byte
		if _, err := io.ReadFull(r, extra[:]); err != io.EOF {
			if err != nil {
				return result, fmt.Errorf("read automaton: %w", err)
			}
			return result, fmt.Errorf("%w: document exceeds %d bytes", ErrInvalidJSON, limits.MaxBytes)
		}
	}
	if !utf8.Valid(raw) {
		return result, fmt.Errorf("%w: document is not UTF-8", ErrInvalidJSON)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc jsonDocument[S]
	if err := decoder.Decode(&doc); err != nil {
		return result, fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return result, fmt.Errorf("%w: trailing content", ErrInvalidJSON)
	}
	if doc.Version != 1 || (doc.Kind != KindDFA && doc.Kind != KindNFA) {
		return result, fmt.Errorf("%w: unsupported version %d or kind %q", ErrInvalidJSON, doc.Version, doc.Kind)
	}
	if doc.StateCount <= 0 || doc.StateCount > limits.MaxStates || doc.Start == nil {
		return result, fmt.Errorf("%w: invalid stateCount or missing start", ErrInvalidJSON)
	}
	def := Definition[S]{StateCount: doc.StateCount, Start: *doc.Start}
	if len(doc.Accepting) != 0 {
		def.Accepting = make([]int, len(doc.Accepting))
	}
	for i, state := range doc.Accepting {
		if state == nil {
			return result, fmt.Errorf("%w: accepting state %d must be an integer", ErrInvalidJSON, i)
		}
		def.Accepting[i] = *state
	}
	def.Transitions = make([]Transition[S], len(doc.Transitions))
	for i, edge := range doc.Transitions {
		if edge.From == nil || edge.To == nil || edge.Symbol == nil {
			return result, fmt.Errorf("%w: transition %d requires from, to and symbol", ErrInvalidJSON, i)
		}
		def.Transitions[i] = Transition[S]{From: *edge.From, To: *edge.To, Symbol: *edge.Symbol}
	}
	epsilon := make([]EpsilonTransition, len(doc.Epsilon))
	for i, edge := range doc.Epsilon {
		if edge.From == nil || edge.To == nil {
			return result, fmt.Errorf("%w: epsilon transition %d requires from and to", ErrInvalidJSON, i)
		}
		epsilon[i] = EpsilonTransition{From: *edge.From, To: *edge.To}
	}
	return JSONDefinition[S]{Kind: doc.Kind, Definition: def, Epsilon: epsilon}, nil
}

// WriteJSON writes a definition in the version-1 JSON format. Call Compile
// first if the definition has not already been validated.
func WriteJSON[S JSONSymbol](w io.Writer, def JSONDefinition[S]) error {
	doc := jsonDocument[S]{
		Version: 1, Kind: def.Kind, StateCount: def.StateCount, Start: &def.Start,
		Accepting:   make([]*int, len(def.Accepting)),
		Transitions: make([]jsonTransition[S], len(def.Transitions)),
		Epsilon:     make([]jsonEdge, len(def.Epsilon)),
	}
	for i := range def.Accepting {
		doc.Accepting[i] = &def.Accepting[i]
	}
	stringSymbols := reflect.TypeFor[S]().Kind() == reflect.String
	for i := range def.Transitions {
		edge := &def.Transitions[i]
		if stringSymbols && !utf8.ValidString(reflect.ValueOf(edge.Symbol).String()) {
			return fmt.Errorf("%w: transition %d symbol is not UTF-8", ErrInvalidJSON, i)
		}
		doc.Transitions[i] = jsonTransition[S]{jsonEdge: jsonEdge{&edge.From, &edge.To}, Symbol: &edge.Symbol}
	}
	for i := range def.Epsilon {
		edge := &def.Epsilon[i]
		doc.Epsilon[i] = jsonEdge{&edge.From, &edge.To}
	}
	return json.NewEncoder(w).Encode(doc)
}
