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

## Event-driven workflows

Use the `workflow` subpackage to run a named state graph with declarative
conditions and optional weighted selection. The package uses the standard
library and runs independently of the DFA and NFA matchers.

### Load and run a definition

[The order workflow](workflow/testdata/order.json) is a complete version-1
document. It sends high-risk orders to manual review and chooses a shipping
path for other orders. This example assumes `reader` supplies that document.

```go
import "github.com/breskos/gopher-state/workflow"

engine, err := workflow.ReadJSON(reader, 1<<20)
if err != nil {
    return err
}

decision, ok, err := engine.Step(engine.Start(), "submit", workflow.Facts{
    "risk": workflow.Number(25),
}, nil)
if err != nil {
    return err
}
if ok {
    // Save decision.To as the current state for the next event.
}
```

The document declares `version: 1`, a `start` state, a `states` array,
and a `transitions` array. Each transition specifies a unique `id`, `from`,
`to`, and `event`. A transition without `when` always qualifies when its
source and event match. Use `Compile` to build the same graph from Go values.
`ReadJSON` enforces a caller-provided byte limit, rejects unknown configuration
fields, and validates the graph before returning an engine.

The compiled engine is immutable. Keep each instance's current state in your
application. Call `Step` when an event arrives with its current facts. If a
condition becomes true later, send another event with the updated facts. For
example, a scheduler can send `deadline-reached` with `elapsedSeconds`. When
two conditions are true for the same event, the group's selection policy
decides. An unmatched event yields `ok == false`. `Run` processes a list of
events from the start and returns the selected transition IDs, the last state,
and how many inputs were consumed before it stopped. Event execution performs
one transition per input. Define a subsequent event for the next move.

### Conditions and selection

A `when` object contains exactly one of the following forms.

| Form | Example |
| --- | --- |
| Field comparison | `{"field":"risk","op":"gte","value":80}` |
| All conditions must hold | `{"all":[{"field":"approved","op":"eq","value":true},{"field":"score","op":"gte","value":10}]}` |
| At least one condition must hold | `{"any":[{"field":"vip","op":"eq","value":true},{"field":"coupon","op":"exists"}]}` |
| Negate a condition | `{"not":{"field":"blocked","op":"eq","value":true}}` |

`eq` and `ne` compare values of the same type. `lt`, `lte`, `gt`, and
`gte` compare numbers or quantities with matching units. `exists` checks the presence of a field, including a
field with value `null`, and takes no `value`. Missing facts and type
mismatches make comparisons false. Negation inverts the result of its
operand, so a negated comparison can also match a missing field. Use
`exists` alongside it if presence is required. Field names are exact map keys.
Facts can also be decoded from JSON into `workflow.Facts`. Numbers use binary64.
Use numbers within the exact integer range when comparing integer quantities.
Boolean groups must be nonempty and can be nested up to 64 levels.

All transitions for a given source and event use the same selection policy.

- **`first`** is the default. The first eligible transition in JSON order
  wins. Put an unconditional fallback last.
- **`weighted`** requires a positive integer `weight` on every transition
  in the group. For eligible weights 1 and 3, the probabilities are 25% and
  75%. If only the second transition qualifies, it wins every time.
  The engine draws only when at least two transitions qualify. Pass
  `nil` for the default random source, or pass a seeded generator's
  `Uint64N` method for reproducible runs. Keep facts stable during a call.
  Use separate seeded generators per instance or synchronize shared access.

### Transition metadata

The optional `metadata` object holds application-defined JSON. The order
example stores `estimate: "5m"` and a cost object with `currency: "EUR"` and
`minorUnits: 250`. Another application can store resources, scores, or its own
fields. Use `engine.Metadata(decision.Transition)` to read metadata for a
selected edge. If a metadata value should affect eligibility, supply it as a
fact and reference it in a condition or requirement.

The engine indexes candidates by current state and event when compiling the
graph. `Step` can run without heap allocations for both selection policies.
Use `go test -run '^$' -bench '^BenchmarkStep$' -benchmem ./workflow` to
measure the example's dispatch paths.

### Example: a virtual person plans an outing

Run the [person simulation](examples/person/main.go) from the repository root.
Its complete decision graph is in [person.json](examples/person/person.json).

```sh
# Both shops are reachable. A seeded draw chooses between them.
go run ./examples/person -budget 1000 -reserve 200 -seed 1

# A 240 m round-trip limit leaves the bakery as the only shopping option.
go run ./examples/person -budget 1000 -max-meters 240

# A low budget leaves the free park visit. Add rain and the person stays home.
go run ./examples/person -budget 300
go run ./examples/person -budget 300 -rain
```

The person keeps a cash reserve, has separate limits for distance and elapsed
time, and converts walking distance into seconds at a configurable speed.
The application derives eligibility facts from the current wallet, time
budget, and transition metadata. The JSON graph selects the route, selects
a purchase at the destination, and models the return home. If both shops
qualify they have weights 3 and 1. At the market the person buys lunch first
if it fits the remaining budget and time, or buys cheaper fruit if that fits.
The fallback transitions cover a park visit or staying home.

The command prints the selected transitions, final wallet, meters walked,
and elapsed seconds. The example tests use controlled random draws to verify
both routes, exact budget and time boundaries, the weather fallback, and a
price change made only in the JSON configuration.

### Run the person simulation on multiple cores

```sh
go run ./examples/person -runs 100000 -workers 4 -seed 42
go run ./examples/person -runs 100000 -workers 1 -seed 42
```

Both commands print the same route counts and totals. Each run's random
stream is keyed by the base seed and zero-based run ID. Worker scheduling and
worker count have no effect on the result. A worker count of zero uses Go's
available parallelism. Use `-summary` to aggregate even a single run. The
ordinary one-person command prints its full path.

The command loads the graph and decodes prices, walking distances and activity
durations once. Workers share that immutable data and accumulate their own
counters. Batch mode omits individual paths. It merges only the counters for
route choices, spending, distance and elapsed time.

For other independent simulations, `workflow.RunBatch` supplies each worker
with a contiguous range of run IDs. Keep mutable state local to the callback
and check its context between runs to support cancellation. The caller
chooses how to generate seeds and combine results. If simulated people share
inventory or otherwise interact, define a consistent event order inside each
world. Use batches to run independent worlds in parallel.

Reproduce the single-trip and multi-core measurements locally:

```sh
go test -run '^$' -bench 'Benchmark(FullTrip|PersonBatch)$' -benchmem ./examples/person
```

The batch benchmark reports runs per second for 1, 2, 4 and 8 workers.
Actual scaling depends on available CPU and how much work each trip performs.

### Resource requirements, units and execution probability

A transition can declare multiple minimum requirements in `requires`.
Every requirement and the optional `when` expression must pass. For
example, this edge requires a budget of 250 euro cents and 2 liters of
water, and compares the route distance to the person's current limit:

```json
{
  "id": "visit-market",
  "from": "home",
  "to": "market",
  "event": "plan",
  "requires": [
    {"field": "wallet", "atLeast": {"amount": 250, "unit": "EUR-cent"}},
    {"field": "water", "atLeast": {"amount": 2, "unit": "L"}}
  ],
  "when": {"field": "distance", "op": "lte", "otherField": "walkingLimit"},
  "probability": 0.8
}
```

[The complete graph](workflow/testdata/resource-graph.json) and
[the input event](workflow/testdata/resource-input.json) are both JSON.
The executable Go example loads both with `ReadJSON` and
`json.Unmarshal` into `workflow.Input`. Every connection can use different
fact names, thresholds, boolean expressions, and probabilities.

- Facts and condition literals accept scalars or quantities such as
  `{"amount": 300, "unit": "m"}`. Quantity comparisons require identical unit
  names. Convert units before supplying facts if the sources use different
  conventions. Missing facts, incompatible types or units fail a comparison.
- A comparison takes either a literal `value` or an `otherField` referring
  to another current fact. For example, the person simulation compares its
  spendable balance and shopping time to prices and durations from the JSON
  model. Minimum requirements accept nonnegative numbers or quantities.
- Eligibility is checked first. The engine selects among eligible edges
  using `first` or relative `weight`, then applies the selected edge's
  optional absolute `probability` from 0 to 1. Omit it for certainty. A
  failed probability check leaves the state unchanged for that event.
  With weights 1 and 3 and probabilities 0.8 and 1, if both edges are eligible,
  their execution chances are 20% and 75%, with a 5% chance of staying put.
  Fractional probabilities use a 53-bit uniform draw.
- Requirements check availability. The caller applies effects such as
  debiting a wallet or reserving shared inventory after a successful
  decision. Supply the resulting facts on the next event.

### Add states while people are running

Create a `workflow.World` from a compiled engine, then use `world.NewRun`
for each person. A run owns its current state, sequence number and optional
random source. Pass a unique ID for runs sharing a log. Different runs can
advance concurrently against the same world. Calls on one run are serialized,
so the caller should submit events in the desired order.

```go
world, err := workflow.NewWorld(engine)
if err != nil {
    return err
}
var history workflow.MemoryRecorder
person, err := world.NewRun("person-17", nil, &history)
if err != nil {
    return err
}

if _, ok, err := person.Step("walk", nil); err != nil || !ok {
    return fmt.Errorf("walk did not advance: accepted=%t, error=%v", ok, err)
}
if err := world.AddJSON(newMarketReader, 16<<10); err != nil {
    return err
}
decision, ok, err := person.Step("explore", workflow.Facts{
    "wallet":     workflow.Quantity(400, "EUR-cent"),
    "marketOpen": workflow.Bool(true),
})
```

The [base graph](workflow/testdata/live-base.json) starts with a route to the
square. The [JSON patch](workflow/testdata/live-market.json) adds two
destinations and their edges while the person is already there. A patch has
`states` and `transitions` arrays. It can refer to existing states or states
created in the same patch. All changes become visible together to subsequent
steps. Existing transitions keep their order, and a patch appends any new
transitions to the group. The group's selection policy must remain consistent.
An invalid patch leaves the previous graph in place. Use one world per
independent simulation when graph changes should remain local to that simulation.

### Record each person's transitions

Pass `nil` to `NewRun` for a run without logging. Choose
`MemoryRecorder` and call `Records()` for an in-process snapshot, or pass
`NewJSONLRecorder(writer)` to store one JSON record per line in a file or
stream. Use a shared recorder for several people. Records carry the run ID
and a sequence number starting at one, so each person's path can be recovered
even when writes from different people interleave.

A record contains the source, destination, event, edge ID, effective
condition including requirements, facts referenced by that condition, and the
edge's selection mode, weight and execution probability. It records a
successful transition after evaluation. The log excludes other event facts
and rejected candidates. A `RecorderFunc` lets applications filter sensitive
facts or store records through their own persistence layer.

A recorder error is returned by `Step` before the run advances. A retry
evaluates the event again and may use another random draw. The caller owns
the JSONL writer and controls flushing, syncing and closing. A file write
may fail after writing some bytes. Applications requiring atomic persistence
of the transition log and external effects should handle both in their own
storage transaction.

### Private graphs and shared encounters

Give each person a run in their own private world. Open a
`workflow.Encounter` when those people should see a public graph together,
and close it when the simulated meeting ends. The application controls
when meetings open and close, for example from simulation events or ticks.

A `Binding` explicitly maps shared state IDs to local state IDs for a
participant. For example, both `mia-meeting-point` and `noah-meeting-point`
can map to the public state `square`. Each person also maps the public
destinations they can reach to states in their own graph. A person outside
the encounter, outside their mapped positions, or without a mapping for an
edge's destination has no access to that shared edge. To make a public
location available to everyone in a scene, bind every intended participant.

- **Show choices:** `encounter.Options(run, event, facts)` lists eligible
  private and shared edges with their scope, local target, and optional shared
  target. This call neither changes state nor draws randomness.
- **Decide individually:** `encounter.Step(run, scope, event, id, facts)`
  takes one person's private or shared edge. An empty ID uses that graph's
  selection policy. An explicit ID chooses that eligible edge and applies its
  execution probability. Other people remain in their current states.
- **Act together:** `encounter.Together` receives the consenting participants
  with their own facts. They must be at the same shared position, and the
  selected shared edge must be eligible and mapped for all of them. Weighted
  selection and execution probability are each drawn once for the group.
  Each participant advances to their own mapped target only after the joint
  action succeeds. Missing eligibility, a failed draw, or a recorder error
  leaves the participants in their original states.

The [festival graph](workflow/testdata/shared-festival.json) and
[executable encounter example](workflow/encounter_test.go) demonstrate this.
Mia sees her private library option and Noah sees his private bike-shop option.
Both see the public food stall, and each sees the concert only if their wallet
meets the ticket requirement. If they agree to act together and only Mia can
afford the concert, the food stall is the common choice.

For logged joint actions, pass a `GroupRecorder` to `Together`. Only runs
with logging enabled contribute records. The group recorder receives those
records in a single call, before states advance. A shared `MemoryRecorder`
can use the same history for individual and joint moves. `JSONLRecorder`
writes each joint action as one JSON line containing a `group` array.
Shared records include the encounter ID, the public edge and the local
decision. A recorder or random callback should not reenter the participating
runs or encounter. For transactions involving external effects or durable
state, use a group recorder backed by application storage.

#### Walk through the festival example

```sh
go run ./examples/encounter
go run ./examples/encounter -individual
```

Both commands load [festival.json](examples/encounter/festival.json). This
example uses a small application-level JSON envelope around the usual workflow
definitions:

| Field | What it configures |
| --- | --- |
| `public` | The festival graph shared by the enrolled participants. |
| `people[].id` | A unique run ID within the encounter and a key for grouping logs. |
| `people[].private` | The person's own versioned workflow graph. |
| `people[].facts` | Facts supplied for the person's decision, such as their wallet. |
| `people[].binding` | A map from a public state ID to a state ID in that person's private graph. |

The example program decodes this envelope, compiles each definition, creates
one private world per person, and opens an encounter with the public world.
You can use the same pattern with separate JSON files. The envelope is specific
to the example. The workflow package accepts individual definitions through
`Compile` or `ReadJSON` and bindings through `NewEncounter`.

Before either command takes a step, it prints the options:

```text
Mia's choices:
  private: visit-library -> library
  shared: see-concert -> mia-concert
  shared: visit-food-stall -> mia-food
Noah's choices:
  private: visit-bike-shop -> bike-shop
  shared: visit-food-stall -> noah-food
```

In the first command, both people consent to a joint action. Noah has 300
euro cents and the concert requires 500 per person. The food stall becomes
the only common option. The public edge is the same for both people, but
their local destinations stay distinct:

```text
Mia #1: mia-meeting-point --visit-food-stall--> mia-food
Noah #1: noah-meeting-point --visit-food-stall--> noah-food
```

In the second command, Mia explicitly takes her private library edge and Noah
explicitly takes the shared food-stall edge. Mia's choice has no effect on
Noah's state. The command closes the encounter and prints the recorded paths.

#### Integrate encounters in a simulation

1. **Keep the runs.** Create one `Run` per actor. When an actor belongs to a
   private graph, give that run its own `World`. You can reuse a compiled
   engine as the starting point for several worlds. Extending one world
   leaves the others unchanged.
2. **Open a meeting when the model calls for it.** Pass the public world and
   the participants' bindings to `NewEncounter`. The constructor copies the
   bindings and checks that the mapped states exist. A local state maps to
   at most one shared state per encounter. Two unrelated local state names
   can refer to the same shared location.
3. **Provide facts at the time of the event.** Options are a snapshot and
   carry no reservation. A later `Step` or `Together` checks the current
   states, mappings and supplied facts again. For example, a person may
   have spent money since the options were shown. If the public world is
   extended, subsequent calls see the new edges between mapped states.
   To add participants or mappings, open a new encounter with those bindings.
4. **Choose an execution mode.** Use `Options` for a UI or policy that
   chooses between the two scopes. Use `Step` for one run. An empty edge ID
   lets the graph apply its own policy within the selected scope. Pass an
   explicit edge ID to select an eligible option directly. The absolute
   execution probability still applies. Use `Together` with the consenting
   runs and each run's own facts for a joint decision. Its returned decisions
   follow the order of the supplied participants.
5. **Close the meeting.** Close removes shared visibility for every
   participant in that encounter. People keep their current local states and
   can continue on private edges. The caller schedules encounter boundaries
   and applies effects like charging wallets or updating inventory.

If both festival participants can afford tickets, the weighted choice gives
the concert a 3/4 chance of being selected. Its execution probability is 0.8,
so the resulting chances are 60% to attend together, 25% to visit the food
stall together, and 15% to stay in place. A failed execution draw does not
try a second edge. In an independent action only the acting person's
eligibility and random source are used. In a joint action the supplied group
draw is used for the whole group.

To recover the public route from recorded transitions, read the encounter ID
and the `shared` decision. To recover a person's path, group records by
`runId` and order them by `sequence`. A shared JSONL file may have ordinary
record objects and objects containing a `group` array. The group line is a
single write of the logged participants in one successful joint action.
Recorders and draw callbacks run synchronously. Keep them short and avoid
calling back into the encounter or its runs.

#### Verify all examples and measure coverage

```sh
go test -race ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go tool cover -html=coverage.out
go vet ./...
go test -run '^$' -fuzz '^FuzzEncounterJointSelection$' -fuzztime=10s ./workflow
```

The CLI tests verify the choices printed before a decision and the local
destinations after joint and independent actions. The workflow tests also
exercise missing mappings, invalid bindings, failed eligibility, invalid
random draws, closure, concurrent joint actions, and group recorder failures.
The encounter fuzz test varies both wallets and both kinds of random draw
and checks the joint decision against the festival's expected rules.

### Let an external processor generate actions and graph branches

A processor in a separate Go project can call `run.Options(event, facts)`,
score the returned edges, and submit the chosen ID through
`run.StepTransition(event, id, latestFacts)`. The ordinary `run.Step` API
still lets the graph choose. An explicit choice bypasses relative weights
and declaration order. Its conditions, minimum requirements, and absolute
execution probability still apply. Option lists reserve nothing: supply
the latest facts when executing the action.

Use `run.ApplyAction` to attach a graph extension to an explicit choice or
`run.ApplyActionJSON(reader, maxBytes)` to accept a bounded JSON action
from a processor. The action contains an `event`, a `transition` ID, the
current `facts`, and an optional `patch` containing new states and
transitions. The chosen edge can be an existing edge or an edge declared
in that patch. For example, [the discovery action](workflow/testdata/discover-action.json)
creates a hidden garden, a greenhouse, and a fountain. It enters the garden
only if the person has a key, then exposes two new weighted options.

The engine validates the patch against the latest world before evaluating
the chosen edge. It publishes the complete extension when the transition
and its optional record succeed. When validation fails, a requirement no
longer holds, a probability draw rejects the transition, or the recorder
reports an error, the engine retains the old graph and run state. Existing
world snapshots are immutable. Other runs in the same world see the new
branches on subsequent steps. Concurrent actions with distinct IDs
retain both extensions. Reusing a state or edge ID is rejected.

The [executable Go example](workflow/action_test.go) demonstrates the exchange
between a processor and a run. The application supplies facts, generates IDs,
and applies any inventory, time, or other external effects after success.
An action's input JSON is separate from the transition audit log. Persist the
action or patch through the application if the generated graph must be rebuilt
later. Draw and recorder callbacks should avoid reentering the run or modifying
its world while an action is in progress.

#### Run the processor example

From the repository root:

```sh
go run ./examples/processor workflow/testdata/live-base.json workflow/testdata/discover-action.json
```

```text
Discovery: square -> hidden-garden
Option: visit-greenhouse (weight 3)
Option: visit-fountain (weight 1)
Processor chose visit-fountain; explorer is now at fountain
```

The [base graph](workflow/testdata/live-base.json) only knows about home and
the square. The [processor command](examples/processor/main.go) walks the
explorer to the square, then reads a proposed action. That action defines
the garden and both destinations. The normal weighted policy favors the
greenhouse, while the example's external policy deliberately selects the
fountain. To see the rejection path, copy the action file, change
`"hasKey": true` to `"hasKey": false`, and pass the copy to the command.
The explorer stays at the square and the proposed states remain unpublished.

#### Build the processor boundary in another Go project

Import `github.com/breskos/gopher-state/workflow`. Keep the processor's
ranking, generation and application data in the consuming project. Supply
named facts that the graph can read, such as location visibility, keys or
available resources. Use this sequence for each decision:

1. Read the candidate edges with `run.Options(event, facts)`. This does not
   draw randomness. Use each option's transition ID to make or retain a
   choice in your processor.
2. Recalculate facts before submitting the choice if the world has changed.
   Use `StepTransition` for an existing edge, or assemble an `Action`
   with a patch if the result needs new graph nodes. A transition declared
   in the patch can target a state declared there as well.
3. Check the returned result. On `ok == true`, the decision contains the
   actual source, target and chosen edge. It is now safe for the application
   to proceed with the effects it owns. On `ok == false` with a nil error,
   the requested edge was unavailable, its conditions failed or its
   probability rejected the move. On an error, inspect the validation,
   randomness or recorder failure. In both failure cases the generated
   patch remains unpublished.

| Action JSON field | Contract |
| --- | --- |
| `event` | Event under which the chosen edge is declared. |
| `transition` | Required explicit edge ID. It must be available from the run's current state for that event. |
| `facts` | Optional current facts for evaluating guards and minimum requirements. |
| `patch.states` | Optional list of new state IDs. Allocate stable, unique IDs in the processor. |
| `patch.transitions` | Optional list of new edges. They can connect existing states, new states, or both. |

A world is append-only. Replaying a successful action that creates the same
IDs conflicts with those existing IDs. Applications that retry across process
or network failures should keep their own action IDs and record which ones
committed. Persist the original graph and accepted patches in order if the
generated graph must be reconstructed. Use a separate transaction in your
storage layer when external resource effects and durable history must commit
together.

### Save, resume and explain workflows (v0.3.0)

The [acquaintance example](examples/acquaintance/relationship.json) models
a directed relationship from stranger to close friend, with a downgrade
after a long period without contact:

```sh
go run ./examples/acquaintance
```

- **Save a run (GST-100–103).** `run.Snapshot()` captures its ID, current
  state and sequence. `world.ResumeRun(snapshot, draw, recorder)` restores
  that position and continues numbering records. The state must still be
  present in the graph, including when it has been retired. Keep random
  seeds or state and application facts in the application. Once the saved
  sequence reaches the maximum uint64 value, a successful transition returns
  `ErrSequenceExhausted` without changing the run or publishing its patch.
- **Save a world (GST-110–114).** `world.Snapshot().Definition()` returns a
  compilable copy in insertion order. For provenance and duplicate patch ID
  detection after a restart, keep the original base definition and
  `world.Patches()`, then call `RestoreWorld(base, patches)`. The current
  definition already incorporates those patches. Pause relevant writes
  while taking a coordinated world and run save. Individual snapshots
  are thread-safe. `TransitionOrigin` returns the patch's ID, source and note
  for added edges. `Graph()` includes these details for nodes and edges.
- **Supply per-step context (GST-200–203).** `StepWith`,
  `StepTransitionWith`, `ApplyActionWith` and encounter `StepWith` take
  `StepOptions`. If configured, its draw overrides the run's draw for
  that call. `TogetherWith` uses one draw source for the group. The `Meta`
  map is copied into each successful record. The existing methods preserve
  their default behavior.
- **Explain choices (GST-300–303).** `Explain` on an engine, run or encounter
  returns candidates without drawing randomness or advancing the run.
  Eligible candidates match `Options`. Failures include paths such as
  `when.all[1]` and `requires[0]`. A failed `any` reports all rejected
  branches. A failed `not` reports that its operand held. Zero probability
  is reported as `probability`; it does not change the original selection
  policy. Encounter explanations cover private edges and mapped shared edges.
- **Retire parts of the graph (GST-400–403).** Use `Patch.Retire.States` or
  `Patch.Retire.Transitions`. Retired parts remain in definition exports
  and patch history. Edges entering retired states are unavailable, while
  people already there can leave through eligible edges. To replace a
  retired edge, create a new edge with a new ID and an active target.
- **Inspect the graph (GST-500).** `Engine.Graph()` exposes ordered node
  and edge copies for a renderer. It flags edges whose destinations are
  retired and reports publication provenance. `Definition()` is the
  round-trip format; it preserves the declared edge retirement separately
  from retirement of its target.

The example uses JSON in memory. Consumers decide where to store it and
when to restore. See the [changelog](CHANGELOG.md) for migration notes.

#### Render a workflow graph with Graphviz

`engine.Graph().WriteDOT(writer)` exports a workflow as a directed DOT
graph. The start state has an incoming arrow, labels contain the event and
transition ID, and retired states and edges have dashed outlines. The export
uses the current graph snapshot and returns write and graph validation errors.

```go
if err := engine.Graph().WriteDOT(os.Stdout); err != nil {
    return err
}
```

Save the output as `workflow.dot` and use `dot -Tsvg workflow.dot -o workflow.svg`
with Graphviz installed. DOT export uses the Go standard library and does not
require Graphviz at runtime.

CI runs the DFA/NFA fuzz target and both GST-701 and GST-702 workflow
restoration fuzz targets on stable Go. The local verification commands above
can be used to run them for longer.

## License

gopher-state is licensed under the [Apache License, Version 2.0](LICENSE).
