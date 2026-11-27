package gopherstate

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestLazyMatcherCacheBudgets(t *testing.T) {
	nfa, err := CompileNFA(NFADefinition[byte]{
		Definition: Definition[byte]{StateCount: 3, Accepting: []int{2},
			Transitions: []Transition[byte]{{Symbol: 'a', To: 1}, {From: 1, Symbol: 'b', To: 2}}},
		Epsilon: []EpsilonTransition{{From: 1, To: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, limits := range []LazyDFALimits{{1, 8}, {4, 1}, {4, 8}} {
		lazy, err := nfa.NewLazyMatcher(limits)
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			for input, want := range map[string]bool{"ab": true, "aab": true, "a": false, "x": false, "abx": false, "": false} {
				if got := lazy.Match([]byte(input)); got != want {
					t.Errorf("limits %v, input %q: got %t, want %t", limits, input, got, want)
				}
			}
		}
		if len(lazy.states) > limits.MaxStates || lazy.transitions > limits.MaxTransitions {
			t.Fatalf("cache exceeded limits: %d states, %d transitions", len(lazy.states), lazy.transitions)
		}
		if cached, ok := lazy.states[0].row.step('x'); limits.MaxTransitions == 8 && (!ok || cached != 0) {
			t.Fatalf("rejecting transition was not cached: %d, %t", cached, ok)
		}
	}
	if _, err := nfa.NewLazyMatcher(LazyDFALimits{1, 0}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("invalid limits: %v", err)
	}
	hot, err := nfa.NewLazyMatcher(LazyDFALimits{4, 8})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("aab")
	hot.Match(input)
	if allocs := testing.AllocsPerRun(50, func() { hot.Match(input) }); allocs != 0 {
		t.Fatalf("warm lazy matcher allocated %.1f times per call", allocs)
	}
}

func TestBitsetMatcherState63(t *testing.T) {
	def := NFADefinition[byte]{
		Definition: Definition[byte]{StateCount: 64, Accepting: []int{63},
			Transitions: []Transition[byte]{{From: 63, Symbol: 'a', To: 0}, {From: 63, Symbol: 'b', To: 63}}},
		Epsilon: []EpsilonTransition{{From: 0, To: 63}, {From: 63, To: 0}},
	}
	nfa, err := CompileNFA(def)
	if err != nil {
		t.Fatal(err)
	}
	bitset, err := nfa.NewBitsetMatcher()
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]bool{"": true, "ab": true, "bab": true, "ax": false} {
		if got := bitset.Match([]byte(input)); got != want {
			t.Errorf("input %q: got %t, want %t", input, got, want)
		}
	}
	def.StateCount++
	nfa, err = CompileNFA(def)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nfa.NewBitsetMatcher(); !errors.Is(err, ErrStateLimit) {
		t.Fatalf("65 states: %v", err)
	}
}

var benchmarkMatchResult bool

func BenchmarkNFAMatchers(b *testing.B) {
	for _, active := range []int{1, 4, 32} {
		def := NFADefinition[byte]{Definition: Definition[byte]{StateCount: active, Accepting: []int{active - 1}}}
		for from := range active {
			def.Transitions = append(def.Transitions, Transition[byte]{From: from, Symbol: 'a', To: from})
			if from+1 < active {
				def.Epsilon = append(def.Epsilon, EpsilonTransition{From: from, To: from + 1})
			}
		}
		nfa, err := CompileNFA(def)
		if err != nil {
			b.Fatal(err)
		}
		bitset, err := nfa.NewBitsetMatcher()
		if err != nil {
			b.Fatal(err)
		}
		lazy, err := nfa.NewLazyMatcher(LazyDFALimits{MaxStates: 64, MaxTransitions: 256})
		if err != nil {
			b.Fatal(err)
		}
		input := bytes.Repeat([]byte{'a'}, 1024)
		lazy.Match(input)
		for _, variant := range []struct {
			name  string
			match func([]byte) bool
		}{{"NFA", nfa.NewMatcher().Match}, {"Bitset", bitset.Match}, {"WarmLazy", lazy.Match}} {
			b.Run(fmt.Sprintf("active=%d/%s", active, variant.name), func(b *testing.B) {
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				for b.Loop() {
					benchmarkMatchResult = variant.match(input)
				}
			})
		}
	}
}
