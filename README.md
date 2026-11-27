# gopher-state

A dependency-free Go library for deterministic and nondeterministic finite automata. Compile a definition once, then share the immutable automaton across goroutines. Requires Go 1.25 or later.

```sh
go get github.com/breskos/gopher-state
```

## DFA

This automaton accepts an odd number of `a` symbols.

```go
import state "github.com/breskos/gopher-state"

machine, err := state.CompileDFA(state.Definition[byte]{
    StateCount: 2,
    Start:      0,
    Accepting:  []int{1},
    Transitions: []state.Transition[byte]{
        {From: 0, Symbol: 'a', To: 1},
        {From: 1, Symbol: 'a', To: 0},
    },
})
if err != nil {
    return err
}

machine.Match([]byte("aaa")) // true
machine.Match([]byte("aa"))  // false
```

State IDs range from zero to `StateCount - 1`. Symbols can be any comparable Go type, including bytes, runes, strings, enums, and comparable structs. Use `[]rune` for Unicode code points or `[]string` for pre-tokenized input. Matching consumes the whole input and accepts only if a reachable state is accepting at the end. An absent transition rejects that path. An empty input can match if an accepting state is already reachable without consuming a symbol.

Use `Start`, `Step`, and `IsAccepting` for incremental DFA execution. A definition with invalid states, symbols that cannot be used as map keys or compared to themselves, or conflicting DFA transitions returns an error testable with `errors.Is(err, state.ErrInvalidDefinition)`.

## NFA

This automaton accepts one or more `"go"` tokens followed by `"stop"`. It can follow both `"go"` transitions, and the epsilon edge consumes no token.

```go
machine, err := state.CompileNFA(state.NFADefinition[string]{
    Definition: state.Definition[string]{
        StateCount: 4,
        Start:      0,
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
    return err
}

machine.Match([]string{"go", "go", "stop"}) // true
machine.Match([]string{"go", "stop", "go"}) // false
```

DFA matching makes one transition lookup per symbol and allocates no working memory. NFA matching tracks active states and visits each reachable state at most once per input position, even with epsilon cycles. For repeated NFA matching, use `matcher := machine.NewMatcher()` and call `matcher.Match` with one matcher per goroutine to reuse its working memory.

For repeated matching with an NFA whose reachable subsets fit a known budget, use `machine.Determinize(maxStates)` to compile an equivalent DFA. The number of reachable subsets can grow exponentially. The explicit budget bounds the number of states produced, and an exceeded budget returns `state.ErrStateLimit` via `errors.Is`.

## Existing imports

The mutable, string-based API is still available at `github.com/breskos/gopher-state/dfa`. Its `Run` now checks acceptance after consuming the full input and includes the start and last visited state in the path. `RunChecked` adds an error for an invalid start state or a traversed edge with a missing target. Inspection methods read the current graph. Call `Index` explicitly to refresh the legacy exported lookup maps.

## Development

```sh
go test ./...
go test -race ./...
go test -run '^$' -bench . -benchmem
```

To reproduce the differential fuzz test locally:

```sh
go test -run '^$' -fuzz '^FuzzDeterminize$' -fuzztime=15s -parallel=2 .
```

CI runs tests and vet on stable Go across Linux, macOS, and Windows. It also checks the previous stable release and Go 1.25.0 on Linux. The Linux and macOS jobs enable the race detector.
