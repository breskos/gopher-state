# Automaton JSON format, version 1

A document contains one automaton definition. Produce it from any language, or use `WriteJSON` from Go. The loader bounds the document size and number of states. Compile the loaded definition once and reuse the resulting machine for many inputs.

```json
{
  "version": 1,
  "kind": "nfa",
  "stateCount": 3,
  "start": 0,
  "accepting": [2],
  "transitions": [
    { "from": 1, "symbol": "submit", "to": 2 }
  ],
  "epsilon": [
    { "from": 0, "to": 1 }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `version` | Required integer. Use `1`. |
| `kind` | Required string. Use `"dfa"` or `"nfa"`. |
| `stateCount` | Required positive integer. State IDs are integers from 0 through `stateCount - 1`. |
| `start` | Required state ID. |
| `accepting` | Optional array of accepting state IDs. |
| `transitions` | Optional array of objects with required `from`, `symbol`, and `to` fields. |
| `epsilon` | Optional array of objects with required `from` and `to` fields. Only NFAs may have entries here. |

Use UTF-8 JSON. Write each field once with the spelling shown above. Optional arrays may be omitted or empty. The symbol type is chosen when calling `ReadJSON[S]`. Use JSON strings for `string`, or JSON integers in the selected Go type's range for integer symbols. For example, a byte transition for ASCII `a` uses `"symbol":97` with `ReadJSON[byte]`. For a document that needs to load across 32-bit and 64-bit platforms, prefer fixed-width integer types. A symbol value of `null` is invalid. For a DFA, transitions sharing a source and symbol must have the same target.

```go
def, err := state.ReadJSON[string](reader, state.JSONLimits{
    MaxBytes:  1 << 20,
    MaxStates: 10_000,
})
if err != nil {
    return err
}

machine, err := def.Compile()
if err != nil {
    return err
}

accepted := machine.Match([]string{"submit"})
```

`ReadJSON` validates the format and limits. `Compile` checks state references and transition semantics before execution. Use `errors.Is` with `ErrInvalidJSON` for format and limit errors and `ErrInvalidDefinition` for invalid automata. I/O errors are returned to the caller. `WriteJSON(writer, def)` writes the definition. Validate a newly constructed definition with `Compile` before writing it. Persisted data describes states and transitions. Use the loader's typed definition to select a different execution strategy on each deployment.

To load a byte DFA into a compact transition table, call `ReadJSON[byte]`, check `def.Kind == state.KindDFA` and that `def.Epsilon` is empty, then pass `def.Definition` and a table budget in bytes to `CompileByteDFA`. Use `errors.Is(err, state.ErrTableLimit)` to fall back to `def.Compile()` if the table exceeds the budget.
