package dfa_test

import (
	"reflect"
	"testing"

	"github.com/breskos/gopher-state/dfa"
)

func TestRun(t *testing.T) {
	m := dfa.NewDFA("test")
	if _, _, err := m.RunChecked(nil); err == nil {
		t.Fatal("expected an error for missing start")
	}

	start, final := dfa.NewState("start"), dfa.NewState("final")
	final.SetFinal(true)
	start.AddTransition(final, "go")
	final.AddTransition(dfa.NewState("missing"), "broken")
	m.SetStates([]*dfa.State{start, final})
	m.SetStart(start.Name)

	for _, test := range []struct {
		tokens []string
		path   []string
		accept bool
		err    bool
	}{
		{nil, []string{"start"}, false, false},
		{[]string{"go"}, []string{"start", "final"}, true, false},
		{[]string{"go", "extra"}, []string{"start", "final"}, false, false},
		{[]string{"go", "broken"}, []string{"start", "final"}, false, true},
	} {
		path, accept, err := m.RunChecked(test.tokens)
		if !reflect.DeepEqual(path, test.path) || accept != test.accept || (err != nil) != test.err {
			t.Errorf("RunChecked(%v) = %v, %v, %v", test.tokens, path, accept, err)
		}
	}
}

func TestInspectAfterMutation(t *testing.T) {
	m := dfa.NewDFA("test")
	start, left, right, end := dfa.NewState("start"), dfa.NewState("left"), dfa.NewState("right"), dfa.NewState("end")
	start.AddTransition(left, "a->b")
	left.AddTransition(end, "c")
	start.AddTransition(right, "a")
	right.AddTransition(end, "b->c")
	m.SetStates([]*dfa.State{start, left, right, end})
	m.Index()
	if len(m.StateLookup) != 2 {
		t.Fatalf("snapshot combined distinct symbol pairs: %v", m.StateLookup)
	}
	if got := m.InspectStates("a", "b->c"); !reflect.DeepEqual(got, []string{"right"}) {
		t.Fatalf("InspectStates = %v", got)
	}
	start.AddTransition(right, "new")
	if got := m.InspectSymbols("new"); len(got) != 1 || got[0].To != "right" {
		t.Fatalf("InspectSymbols = %v", got)
	}
}

func TestLegacyConstructionAndIncompleteGraph(t *testing.T) {
	var m dfa.DFA
	start := &dfa.State{Name: "start"}
	end := dfa.NewState("end")
	missing := dfa.NewState("missing")
	start.AddTransitions(end, []string{"b", "a"})
	start.AddTransition(missing, "broken")
	m.SetStates([]*dfa.State{start, end})
	m.SetStart(start.Name)
	m.States["nil"] = nil

	if m.GetStart() != "start" || m.GetState("start") != start || m.GetState("nil") != nil {
		t.Fatal("unexpected start or state lookup")
	}
	end.SetFinal(true)
	if !end.IsFinal() || len(start.GetTransitions()) != 3 {
		t.Fatal("unexpected final state or transitions")
	}
	if _, ok, err := m.Step("absent", "a"); ok || err == nil {
		t.Fatal("step from an absent state should report an error")
	}
	if _, accepted := m.Run([]string{"a"}); !accepted {
		t.Fatal("expected a valid transition to accept")
	}
	m.Index()
	if !m.Indexed || len(m.EdgeLookup) != 2 {
		t.Fatalf("unexpected edge snapshot: %v", m.EdgeLookup)
	}
	if got := m.GetSymbols(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("GetSymbols = %v", got)
	}
	if got := m.InspectSymbols("broken"); len(got) != 0 {
		t.Fatalf("unexpected edge to missing state: %v", got)
	}
}
