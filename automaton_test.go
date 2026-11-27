package gopherstate_test

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	state "github.com/breskos/gopher-state"
)

func TestDFA(t *testing.T) {
	transitions := []state.Transition[byte]{{From: 0, Symbol: 'a', To: 1}, {From: 1, Symbol: 'a', To: 0}}
	accepting := []int{1}
	dfa, err := state.CompileDFA(state.Definition[byte]{StateCount: 2, Accepting: accepting, Transitions: transitions})
	if err != nil {
		t.Fatal(err)
	}
	transitions[0].To = 0
	accepting[0] = 0

	for input, want := range map[string]bool{"": false, "a": true, "aa": false, "aaa": true, "ax": false} {
		if got := dfa.Match([]byte(input)); got != want {
			t.Errorf("Match(%q) = %t, want %t", input, got, want)
		}
	}
	if _, ok := dfa.Step(-1, 'a'); ok {
		t.Fatal("invalid state accepted by Step")
	}
}

func TestInvalidDefinitions(t *testing.T) {
	for _, def := range []state.Definition[int]{
		{},
		{StateCount: 1, Start: 1},
		{StateCount: 1, Accepting: []int{-1}},
		{StateCount: 1, Transitions: []state.Transition[int]{{From: 1}}},
		{StateCount: 1, Transitions: []state.Transition[int]{{To: 1}}},
		{StateCount: 2, Transitions: []state.Transition[int]{{To: 0}, {To: 1}}},
	} {
		if _, err := state.CompileDFA(def); !errors.Is(err, state.ErrInvalidDefinition) {
			t.Errorf("CompileDFA(%+v) = %v", def, err)
		}
	}
	_, err := state.CompileNFA(state.NFADefinition[int]{
		Definition: state.Definition[int]{StateCount: 1},
		Epsilon:    []state.EpsilonTransition{{To: 2}},
	})
	if !errors.Is(err, state.ErrInvalidDefinition) {
		t.Fatalf("invalid epsilon transition: %v", err)
	}
	dfa, err := state.CompileDFA(state.Definition[any]{StateCount: 1, Accepting: []int{0},
		Transitions: []state.Transition[any]{{Symbol: nil}, {Symbol: "ok"}},
	})
	if err != nil || dfa.Match([]any{[]int{1}}) || !dfa.Match([]any{nil, "ok"}) {
		t.Fatalf("unexpected dynamic symbol handling: %v", err)
	}
	_, err = state.CompileDFA(state.Definition[any]{StateCount: 1,
		Transitions: []state.Transition[any]{{Symbol: []int{1}}},
	})
	if !errors.Is(err, state.ErrInvalidDefinition) {
		t.Fatalf("invalid dynamic symbol: %v", err)
	}
}

func TestSymbolAndZeroValueContracts(t *testing.T) {
	var dfa state.DFA[int]
	var nfa state.NFA[int]
	var matcher state.NFAMatcher[int]
	if dfa.Match(nil) || dfa.Match([]int{0}) || nfa.Match(nil) || matcher.Match(nil) {
		t.Fatal("zero value accepted input")
	}
	empty, err := nfa.Determinize(1)
	if err != nil || empty.Match(nil) {
		t.Fatalf("determinization of zero value: %v", err)
	}
	if _, err := nfa.Determinize(0); !errors.Is(err, state.ErrInvalidDefinition) {
		t.Fatalf("invalid state budget: %v", err)
	}
	if _, err := state.CompileDFA(state.Definition[float64]{StateCount: 1,
		Transitions: []state.Transition[float64]{{Symbol: math.NaN()}},
	}); !errors.Is(err, state.ErrInvalidDefinition) {
		t.Fatalf("non-reflexive symbol: %v", err)
	}

	type symbol struct{ Values [1]any }
	good := symbol{Values: [1]any{"go"}}
	bad := symbol{Values: [1]any{[]int{1}}}
	def := state.Definition[symbol]{StateCount: 2, Start: 1, Accepting: []int{0},
		Transitions: []state.Transition[symbol]{{From: 1, Symbol: good}, {From: 1, Symbol: good}},
	}
	dynamicDFA, err := state.CompileDFA(def)
	if err != nil {
		t.Fatal(err)
	}
	dynamicNFA, err := state.CompileNFA(state.NFADefinition[symbol]{Definition: def})
	if err != nil {
		t.Fatal(err)
	}
	converted, err := dynamicNFA.Determinize(2)
	if err != nil {
		t.Fatal(err)
	}
	if next, ok := dynamicDFA.Step(dynamicDFA.Start(), good); !ok || !dynamicDFA.IsAccepting(next) {
		t.Fatal("incremental DFA rejected a valid transition")
	}
	if _, ok := dynamicDFA.Step(dynamicDFA.Start(), bad); ok {
		t.Fatal("incremental DFA accepted an unhashable symbol")
	}
	for _, match := range []func([]symbol) bool{dynamicDFA.Match, dynamicNFA.Match, converted.Match} {
		if !match([]symbol{good}) || match([]symbol{bad}) || match(nil) {
			t.Fatal("unexpected handling of a nested interface symbol")
		}
	}
}

func branchingNFA(size int) state.NFADefinition[byte] {
	return state.NFADefinition[byte]{
		Definition: state.Definition[byte]{
			StateCount: size,
			Accepting:  []int{4},
			Transitions: []state.Transition[byte]{
				{From: 1, Symbol: 'a', To: 1}, {From: 1, Symbol: 'a', To: 2},
				{From: 2, Symbol: 'b', To: 3},
			},
		},
		Epsilon: []state.EpsilonTransition{{From: 0, To: 1}, {From: 1, To: 0}, {From: 3, To: 4}},
	}
}

func TestNFA(t *testing.T) {
	for _, size := range []int{5, 65} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			def := branchingNFA(size)
			nfa, err := state.CompileNFA(def)
			if err != nil {
				t.Fatal(err)
			}
			def.Epsilon[0].To = 4
			def.Transitions[0].Symbol = 'x'
			matcher := nfa.NewMatcher()
			dfa, err := nfa.Determinize(3)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := nfa.Determinize(2); !errors.Is(err, state.ErrStateLimit) {
				t.Fatalf("state limit: %v", err)
			}

			for input, want := range map[string]bool{"": false, "ab": true, "aab": true, "a": false, "ba": false, "abb": false, "xab": false} {
				t.Run(input, func(t *testing.T) {
					t.Parallel()
					if got := nfa.Match([]byte(input)); got != want {
						t.Errorf("NFA.Match = %t, want %t", got, want)
					}
					if got := dfa.Match([]byte(input)); got != want {
						t.Errorf("DFA.Match = %t, want %t", got, want)
					}
				})
				if got := matcher.Match([]byte(input)); got != want {
					t.Errorf("matcher.Match(%q) = %t, want %t", input, got, want)
				}
			}
		})
	}
}

func TestNFAEpsilonAcceptsEmpty(t *testing.T) {
	nfa, err := state.CompileNFA(state.NFADefinition[byte]{
		Definition: state.Definition[byte]{StateCount: 2, Accepting: []int{1}},
		Epsilon:    []state.EpsilonTransition{{From: 0, To: 1}, {From: 1, To: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nfa.Match(nil) || nfa.Match([]byte{0}) {
		t.Fatal("expected only empty input to match")
	}
}

func TestNFALargeFrontier(t *testing.T) {
	const size = 130
	def := state.NFADefinition[byte]{
		Definition: state.Definition[byte]{StateCount: size, Accepting: []int{size - 1}},
	}
	for i := 1; i < size; i++ {
		def.Epsilon = append(def.Epsilon, state.EpsilonTransition{From: i - 1, To: i})
	}
	def.Transitions = []state.Transition[byte]{{From: size - 1, Symbol: 'a', To: 0}}
	nfa, err := state.CompileNFA(def)
	if err != nil {
		t.Fatal(err)
	}
	matcher := nfa.NewMatcher()
	if !matcher.Match([]byte("aaa")) || matcher.Match([]byte("ax")) || !matcher.Match(nil) {
		t.Fatal("unexpected match result when reusing a large frontier")
	}
	dfa, err := nfa.Determinize(1)
	if err != nil || !dfa.Match([]byte("aaa")) || dfa.Match([]byte("ax")) {
		t.Fatalf("unexpected determinization of large frontier: %v", err)
	}
	input := []byte("aaa")
	if allocs := testing.AllocsPerRun(50, func() { matcher.Match(input) }); allocs != 0 {
		t.Errorf("reusable NFA matcher allocated %.1f times per call", allocs)
	}
}

func FuzzDeterminize(f *testing.F) {
	f.Add([]byte{0, 1, 2, 1, 0, 2, 0, 1}, []byte{0, 1})
	f.Add([]byte{}, []byte{})
	f.Fuzz(func(t *testing.T, graph, input []byte) {
		const size = 5
		def := state.NFADefinition[byte]{Definition: state.Definition[byte]{StateCount: size, Accepting: []int{size - 1}}}
		for len(graph) >= 3 && len(def.Epsilon)+len(def.Transitions) < 40 {
			from, to, symbol := int(graph[0]%size), int(graph[1]%size), graph[2]%3
			if symbol == 2 {
				def.Epsilon = append(def.Epsilon, state.EpsilonTransition{From: from, To: to})
			} else {
				def.Transitions = append(def.Transitions, state.Transition[byte]{From: from, To: to, Symbol: symbol})
			}
			graph = graph[3:]
		}
		nfa, err := state.CompileNFA(def)
		if err != nil {
			t.Fatal(err)
		}
		dfa, err := nfa.Determinize(1 << size)
		if err != nil {
			t.Fatal(err)
		}
		if len(input) > 128 {
			input = input[:128]
		}
		input = slices.Clone(input)
		for i := range input {
			input[i] %= 3
		}
		want := referenceMatch(def, input)
		if nfa.Match(input) != want || dfa.Match(input) != want {
			t.Fatalf("compiled automata disagree with reference for %v and %v", def, input)
		}
	})
}

func referenceMatch(def state.NFADefinition[byte], input []byte) bool {
	type position struct{ state, offset int }
	queue := []position{{state: def.Start}}
	visited := make(map[position]bool)
	for i := 0; i < len(queue); i++ {
		p := queue[i]
		if visited[p] {
			continue
		}
		visited[p] = true
		if p.offset == len(input) && slices.Contains(def.Accepting, p.state) {
			return true
		}
		for _, edge := range def.Epsilon {
			if edge.From == p.state {
				queue = append(queue, position{edge.To, p.offset})
			}
		}
		for _, edge := range def.Transitions {
			if edge.From == p.state && p.offset < len(input) && edge.Symbol == input[p.offset] {
				queue = append(queue, position{edge.To, p.offset + 1})
			}
		}
	}
	return false
}

var benchmarkAccepted bool

func BenchmarkMatch(b *testing.B) {
	nfa, err := state.CompileNFA(branchingNFA(5))
	if err != nil {
		b.Fatal(err)
	}
	dfa, err := state.CompileDFA(state.Definition[byte]{
		StateCount: 3,
		Accepting:  []int{2},
		Transitions: []state.Transition[byte]{
			{From: 0, Symbol: 'a', To: 1},
			{From: 1, Symbol: 'a', To: 1},
			{From: 1, Symbol: 'b', To: 2},
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	input := make([]byte, 256)
	for i := range input {
		input[i] = 'a'
	}
	input[len(input)-1] = 'b'
	for name, match := range map[string]func([]byte) bool{
		"DFA": dfa.Match, "NFA": nfa.Match, "NFAMatcher": nfa.NewMatcher().Match,
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				benchmarkAccepted = match(input)
			}
		})
	}
}
