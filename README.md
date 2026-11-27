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

## Choose an execution strategy

| Use case | API |
| --- | --- |
| Protocol or token validation with one successor per symbol | `CompileDFA` and `Match` |
| Repeated byte-stream validation with many outgoing transitions | `CompileByteDFA` with an explicit table budget. It groups equivalent byte values and uses a single flat table. |
| Workflows or grammars with branching or optional steps | `CompileNFA` and `NewMatcher` to reuse scratch memory |
| Small ε-heavy or highly branched NFA with at most 64 states | `NewBitsetMatcher` precomputes ε-closed transition masks. Share the immutable matcher across goroutines. |
| Repeated NFA inputs that visit a small part of a large subset graph | `NewLazyMatcher` caches encountered transitions. Reuse one matcher per goroutine. |
| Repeated NFA matching with few reachable state subsets | `Determinize(maxStates)` and reuse the resulting DFA |
| Store a definition or generate it from another language | [Versioned JSON format](JSON.md) with bounded loading through `ReadJSON` |

Pass explicit cache limits to the lazy matcher:

```go
matcher, err := nfa.NewLazyMatcher(state.LazyDFALimits{
    MaxStates:      256,
    MaxTransitions: 1024,
})
if err != nil {
    return err
}
accepted := matcher.Match(input)
```

The lazy cache starts cold and grows as inputs are matched. The state budget includes the initial subset. The transition budget includes cached rejecting transitions. These limits bound the number of entries. Each cached subset can hold up to the original NFA's state count. If a new entry would exceed either budget, the matcher continues the current input with NFA simulation and keeps existing cache entries available for future runs.

## Run analysis

Call `Analyze(input, state.Observer{})` for acceptance, consumed input length, state visits, symbol and epsilon edge counts, and the maximum number of simultaneously active states. Pass optional callbacks for individual visits. For example, accumulate edge frequencies in a map keyed by `[2]int{event.From, event.To}` inside the `Symbol` callback.

The DFA reports one execution path. The NFA reports its explored alternatives, including branches that later reject. A state is counted once per input offset. An epsilon cycle cannot cause the same state to be explored indefinitely at that offset. Duplicate edge entries count as separate transitions. A symbol edge at offset `i` consumes input `i`. An epsilon edge stays at offset `i`.

Callbacks run synchronously during analysis. Keep callback state local to the run or synchronize shared counters. Use `Match` for regular execution. Observation and statistics are collected only through `Analyze`.

## Implementation and performance

Compilation validates state IDs and symbols and snapshots the definition. DFA and
NFA runs accept only after consuming the complete input. Missing transitions
reject the relevant path. Neither matching nor observation modifies the compiled
automaton.

- **Generic DFA.** Each state stores its first transition inline and uses a map
  for additional symbols. This keeps sparse rows small and supports any
  comparable symbol type.
- **Byte DFA.** During compilation, bytes with identical transition behavior
  across all states are assigned the same column. Table entries store one-based
  row offsets so the match loop uses a class lookup and a table lookup per byte.
  A zero entry means the transition is missing. The budget applies to the
  transition table itself. Accepting flags and temporary compilation memory are
  additional.
- **Standard NFA.** The matcher maintains two reusable slices of active states
  and uses a visited array to deduplicate each frontier, including states reached
  through ε-cycles. Use one `NFAMatcher` per goroutine.
- **Bitset NFA.** For up to 64 states, the compiler folds ε-reachability into
  each transition's destination mask. Matching unions the masks for active
  states and checks the final mask against the accepting states. This trades
  compilation work for a smaller per-match working set.
- **Lazy DFA.** Each matcher interns the ε-closed sets it actually encounters.
  Transitions from those sets are computed on demand and reused across inputs.
  At a cache miss that would exceed either limit, the standard NFA simulation
  continues from the current position. Existing cache entries remain usable.
- **Explicit determinization.** `Determinize` walks all reachable sets up front
  and returns an error if it exceeds the state budget. Use this when the
  resulting state graph is small enough to compile and share.

### Reproduce the measurements

```sh
go test -run '^$' -bench 'BenchmarkByteDFA|BenchmarkCompileByteDFA|BenchmarkNFAMatchers' -benchmem -count=5 .
```

`BenchmarkByteDFA` compares sparse and table-based matching across alphabet
sizes and input lengths. `BenchmarkCompileByteDFA` measures compilation
separately. `BenchmarkNFAMatchers` varies the number of active states and
deliberately warms the lazy cache before timing. Record the CPU and Go version
with results. Cold caches and inputs that exhaust the cache exercise different
paths, so use inputs and cache budgets representative of your application
before choosing a matcher.

The differential fuzz test compares the regular matcher, eagerly determinized
DFA, bitset matcher, bounded lazy matcher, and instrumented execution against
a graph-based reference implementation. The unit tests also cover JSON
round-trips and invalid documents, ε-cycles, cache-budget fallback, and all 256
byte-class IDs.
