package gopherstate_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	state "github.com/breskos/gopher-state"
)

func ExampleReadJSON() {
	const document = `{
		"version": 1,
		"kind": "nfa",
		"stateCount": 3,
		"start": 0,
		"accepting": [2],
		"transitions": [{"from": 1, "symbol": "submit", "to": 2}],
		"epsilon": [{"from": 0, "to": 1}]
	}`
	def, err := state.ReadJSON[string](strings.NewReader(document), state.JSONLimits{MaxBytes: 1024, MaxStates: 10})
	if err != nil {
		fmt.Println(err)
		return
	}
	machine, err := def.Compile()
	if err != nil {
		fmt.Println(err)
		return
	}
	stats := machine.Analyze([]string{"submit"}, state.Observer{
		Symbol: func(event state.Visit) {
			fmt.Printf("%d -> %d\n", event.From, event.To)
		},
	})
	fmt.Println(stats.Accepted, stats.StateVisits, stats.EpsilonTransitions)
	// Output:
	// 1 -> 2
	// true 3 1
}

func TestJSONRoundTrip(t *testing.T) {
	def := state.JSONDefinition[byte]{
		Kind: state.KindDFA,
		Definition: state.Definition[byte]{
			StateCount: 2, Accepting: []int{1},
			Transitions: []state.Transition[byte]{{Symbol: 0, To: 1}, {Symbol: 255, To: 1}},
		},
	}
	var out bytes.Buffer
	if err := state.WriteJSON(&out, def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"symbol":255`) {
		t.Fatalf("unexpected byte encoding: %s", &out)
	}
	decoded, err := state.ReadJSON[byte](&out, state.JSONLimits{MaxBytes: int64(out.Len()), MaxStates: 2})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != def.Kind || !reflect.DeepEqual(decoded.Definition, def.Definition) {
		t.Fatalf("unexpected round trip: %+v", decoded)
	}
	m, err := decoded.Compile()
	if err != nil || !m.Match([]byte{0}) || m.Match([]byte{1}) {
		t.Fatalf("unexpected compiled machine: %v", err)
	}
}

func TestReadJSONValidation(t *testing.T) {
	const prefix = `{"version":1,"kind":"dfa","stateCount":2,"start":0`
	limits := state.JSONLimits{MaxBytes: 1024, MaxStates: 2}
	for _, invalid := range []string{
		`null`, `{}`, `{"version":1,"kind":"dfa","stateCount":2}`,
		`{"version":2,"kind":"dfa","stateCount":2,"start":0}`,
		`{"version":1,"kind":"dfa","stateCount":3,"start":0}`,
		prefix + `,"accepts":[]}`, prefix + `,"accepting":[null]}`, prefix + `} {}`, prefix + `} x`,
		prefix + `,"transitions":[{"from":0,"to":1}]}`,
		prefix + `,"transitions":[{"from":0,"symbol":null,"to":1}]}`,
		prefix + `,"transitions":[{"from":0,"symbol":256,"to":1}]}`,
		prefix + `,"epsilon":[{"from":0}]}`, prefix + "}\xff",
	} {
		if _, err := state.ReadJSON[byte](strings.NewReader(invalid), limits); !errors.Is(err, state.ErrInvalidJSON) {
			t.Errorf("ReadJSON(%q) = %v", invalid, err)
		}
	}
	if _, err := state.ReadJSON[byte](strings.NewReader(prefix+"}"), state.JSONLimits{MaxBytes: int64(len(prefix)), MaxStates: 2}); !errors.Is(err, state.ErrInvalidJSON) {
		t.Fatalf("document limit: %v", err)
	}
	for _, document := range []string{
		prefix + `,"transitions":[{"from":0,"symbol":0,"to":2}]}`,
		prefix + `,"epsilon":[{"from":0,"to":1}]}`,
	} {
		def, err := state.ReadJSON[byte](strings.NewReader(document), limits)
		if err != nil {
			t.Fatal(err)
		}
		if m, err := def.Compile(); m != nil || !errors.Is(err, state.ErrInvalidDefinition) {
			t.Fatalf("Compile = %v, %v", m, err)
		}
	}
}

func TestNFAObservations(t *testing.T) {
	nfa, err := state.CompileNFA(branchingNFA(5))
	if err != nil {
		t.Fatal(err)
	}
	var epsilon, edges []state.Visit
	stats := nfa.Analyze([]byte("ab"), state.Observer{
		Symbol:  func(v state.Visit) { edges = append(edges, v) },
		Epsilon: func(v state.Visit) { epsilon = append(epsilon, v) },
	})
	want := state.RunStats{
		Accepted: true, Consumed: 2, StateVisits: 7, PeakActive: 3,
		SymbolTransitions: 3, EpsilonTransitions: 5,
	}
	if stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	if !reflect.DeepEqual(edges, []state.Visit{{Offset: 0, From: 1, To: 1}, {Offset: 0, From: 1, To: 2}, {Offset: 1, From: 2, To: 3}}) {
		t.Fatalf("symbol edges = %v", edges)
	}
	if len(epsilon) != 5 || epsilon[4] != (state.Visit{Offset: 2, From: 3, To: 4}) {
		t.Fatalf("epsilon edges = %v", epsilon)
	}
	if got := nfa.Analyze([]byte("abx"), state.Observer{}); got.Accepted || got.Consumed != 2 {
		t.Fatalf("rejected run = %+v", got)
	}
}
