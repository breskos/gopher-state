package gopherstate_test

import (
	"fmt"

	state "github.com/breskos/gopher-state"
)

func ExampleCompileDFA() {
	m, err := state.CompileDFA(state.Definition[byte]{
		StateCount: 2,
		Accepting:  []int{1},
		Transitions: []state.Transition[byte]{
			{From: 0, Symbol: 'a', To: 1},
			{From: 1, Symbol: 'a', To: 0},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.Match([]byte("aaa")))
	fmt.Println(m.Match([]byte("aa")))
	// Output:
	// true
	// false
}

func ExampleCompileNFA() {
	m, err := state.CompileNFA(state.NFADefinition[string]{
		Definition: state.Definition[string]{
			StateCount: 4,
			Accepting:  []int{3},
			Transitions: []state.Transition[string]{
				{From: 1, Symbol: "go", To: 1},
				{From: 1, Symbol: "go", To: 2},
				{From: 2, Symbol: "stop", To: 3},
			},
		},
		Epsilon: []state.EpsilonTransition{{From: 0, To: 1}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.Match([]string{"go", "go", "stop"}))
	fmt.Println(m.Match([]string{"go", "stop", "go"}))
	// Output:
	// true
	// false
}
