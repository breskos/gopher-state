package workflow_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

func jsonArchive[T any](t *testing.T, in T) T {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGST103ResumeEquivalence(t *testing.T) {
	for _, mode := range []workflow.Selection{workflow.First, workflow.Weighted} {
		t.Run(string(mode), func(t *testing.T) {
			p := 0.75
			edge := func(id string) workflow.Transition {
				tr := workflow.Transition{ID: id, From: "a", To: "a", Event: "tick", Selection: mode, Probability: &p}
				if mode == workflow.Weighted {
					tr.Weight = 1
				}
				return tr
			}
			base, err := workflow.Compile(workflow.Definition{Version: 1, Start: "a", States: []string{"a"}, Transitions: []workflow.Transition{edge("first"), edge("second")}})
			if err != nil {
				t.Fatal(err)
			}
			execute := func(resume bool) []workflow.Record {
				w, err := workflow.NewWorld(base)
				if err != nil {
					t.Fatal(err)
				}
				var logs workflow.MemoryRecorder
				r, err := w.NewRun("run", nil, &logs)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 9 {
					if i == 2 || i == 6 {
						err := w.Add(workflow.Patch{Info: workflow.PatchInfo{ID: fmt.Sprint(i)}, Transitions: []workflow.Transition{edge(fmt.Sprint("edge-", i))}})
						if err != nil {
							t.Fatal(err)
						}
					}
					if i == 5 && resume {
						state := jsonArchive(t, r.Snapshot())
						w, err = workflow.RestoreWorld(base, jsonArchive(t, w.Patches()))
						if err != nil {
							t.Fatal(err)
						}
						r, err = w.ResumeRun(state, nil, &logs)
						if err != nil {
							t.Fatal(err)
						}
					}
					_, _, err := r.StepWith("tick", nil, workflow.StepOptions{
						Draw: func(limit uint64) uint64 {
							if i%3 == 1 {
								return limit - 1
							}
							return uint64(i) % limit
						},
						Meta: map[string]json.RawMessage{"tick": json.RawMessage(fmt.Sprint(i))},
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				return logs.Records()
			}
			want, got := execute(false), execute(true)
			if len(want) != 6 || !reflect.DeepEqual(want, got) {
				t.Fatalf("records differ: %v vs %v", want, got)
			}
		})
	}
}

func TestGST111And113WorldArchive(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	base := world.Snapshot()
	action := decodeDiscovery(t)
	p := action.Patch
	p.Info = workflow.PatchInfo{ID: "discover", Source: "actor", Note: "garden"}
	if err := world.Add(p); err != nil {
		t.Fatal(err)
	}
	p.States[0] = "changed"
	p.Transitions[0].When.Field = "changed"
	saved := world.Patches()
	saved[0].Transitions[0].When.Field = "also changed"
	saved = jsonArchive(t, world.Patches())
	restored, err := workflow.RestoreWorld(base, saved)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(e *workflow.Engine) []byte {
		out, err := json.Marshal(e.Definition())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := encode(world.Snapshot())
	if !bytes.Equal(first, encode(world.Snapshot())) || !bytes.Equal(first, encode(restored.Snapshot())) {
		t.Fatal("definition bytes changed")
	}
	recompiled, err := workflow.Compile(jsonArchive(t, world.Snapshot().Definition()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recompiled.Options("hidden-garden", "explore", nil), restored.Snapshot().Options("hidden-garden", "explore", nil)) {
		t.Fatal("options changed")
	}
	graph := restored.Snapshot().Graph()
	if graph.Nodes[2].ID != "hidden-garden" || graph.Nodes[2].Patch.ID != "discover" || graph.Edges[1].Patch.ID != "discover" {
		t.Fatalf("graph provenance: %+v", graph)
	}
	if info, ok := restored.Snapshot().TransitionOrigin("enter-hidden-garden"); !ok || info != p.Info {
		t.Fatalf("edge provenance: %v", info)
	}
	if _, err := workflow.RestoreWorld(base, append(saved, saved[0])); !errors.Is(err, workflow.ErrInvalidDefinition) || !strings.Contains(err.Error(), `patch 1 ("discover")`) {
		t.Fatalf("duplicate patch: %v", err)
	}
}

func TestGST401Retirement(t *testing.T) {
	w, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	base := w.Snapshot()
	if err := w.AddJSON(bytes.NewReader(livePatch), 4096); err != nil {
		t.Fatal(err)
	}
	in, err := w.ResumeRun(workflow.RunSnapshot{ID: "inside", State: "pop-up-market", Sequence: 7}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	if err := w.Add(workflow.Patch{Info: workflow.PatchInfo{ID: "close"}, Retire: workflow.Retire{States: []string{"pop-up-market"}, Transitions: []string{"visit-cafe"}}}); err != nil {
		t.Fatal(err)
	}
	facts := workflow.Facts{"wallet": workflow.Quantity(400, "EUR-cent"), "marketOpen": workflow.Bool(true)}
	if len(before.Options("square", "explore", facts)) != 2 || len(w.Snapshot().Options("square", "explore", facts)) != 0 {
		t.Fatal("retirement or snapshot isolation failed")
	}
	for _, c := range w.Snapshot().Explain("square", "explore", facts) {
		if c.Eligible || len(c.Failures) != 1 || c.Failures[0].Kind != "retired" {
			t.Fatalf("retired explanation: %+v", c)
		}
	}
	if _, ok, err := in.Step("leave", nil); err != nil || !ok || in.Snapshot().Sequence != 8 {
		t.Fatalf("leave retired state: %t %v", ok, err)
	}
	after, err := workflow.RestoreWorld(base, w.Patches())
	if err != nil || !reflect.DeepEqual(after.Snapshot().Definition(), w.Snapshot().Definition()) {
		t.Fatalf("retirement restore: %v", err)
	}
	if !slices.Contains(w.Snapshot().Definition().RetiredStates, "pop-up-market") {
		t.Fatal("missing retirement marker")
	}
	replacement := workflow.Transition{ID: "new-link", From: "square", To: "market-cafe", Event: "explore", Selection: workflow.Weighted, Weight: 1}
	if err := w.Add(workflow.Patch{Transitions: []workflow.Transition{replacement}}); err != nil {
		t.Fatal(err)
	}
	if got := w.Snapshot().Options("square", "explore", facts); len(got) != 1 || got[0].Decision.Transition != "new-link" {
		t.Fatalf("replacement: %v", got)
	}
	if err := w.Add(workflow.Patch{Retire: workflow.Retire{Transitions: []string{"visit-cafe"}}}); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("retired twice: %v", err)
	}
}

func FuzzGST701ResumeRun(f *testing.F) {
	f.Add("id", "home", uint64(1))
	f.Add("", "square", uint64(0))
	f.Add("id", "unknown", ^uint64(0))
	f.Fuzz(func(t *testing.T, id, state string, sequence uint64) {
		world, err := loadWorld()
		if err != nil {
			t.Fatal(err)
		}
		s := workflow.RunSnapshot{ID: id, State: state, Sequence: sequence}
		r, err := world.ResumeRun(s, nil, nil)
		switch {
		case id == "":
			if !errors.Is(err, workflow.ErrInvalidDefinition) {
				t.Fatal(err)
			}
		case state != "home" && state != "square":
			if !errors.Is(err, workflow.ErrUnknownState) {
				t.Fatal(err)
			}
		default:
			if err != nil || r.Snapshot() != s {
				t.Fatalf("snapshot changed: %v", err)
			}
		}
	})
}

func FuzzGST702RestoreWorld(f *testing.F) {
	f.Add([]byte(`[{"info":{"id":"close"},"retire":{"states":["home"]}}]`))
	f.Add([]byte(`[{"states":["annex"],"transitions":[{"id":"annex","from":"square","to":"annex","event":"walk"}]}]`))
	f.Add([]byte(`[{"retire":{"transitions":["unknown"]}}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16<<10 {
			return
		}
		var patches []workflow.Patch
		if json.Unmarshal(data, &patches) != nil {
			return
		}
		base, err := loadWorld()
		if err != nil {
			t.Fatal(err)
		}
		w, err := workflow.RestoreWorld(base.Snapshot(), patches)
		if err != nil {
			if !errors.Is(err, workflow.ErrInvalidDefinition) {
				t.Fatal(err)
			}
			return
		}
		again, err := workflow.RestoreWorld(base.Snapshot(), w.Patches())
		if err != nil || !reflect.DeepEqual(w.Snapshot().Definition(), again.Snapshot().Definition()) {
			t.Fatalf("cannot restore accepted history: %v", err)
		}
	})
}

// TestGST100SequenceExhaustion verifies that restoring an extreme sequence
// never wraps audit records or publishes a proposed graph extension.
func TestGST100SequenceExhaustion(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	var records workflow.MemoryRecorder
	run, err := world.ResumeRun(workflow.RunSnapshot{ID: "full", State: "square", Sequence: ^uint64(0)}, nil, &records)
	if err != nil {
		t.Fatal(err)
	}
	before := world.Snapshot()
	action := decodeDiscovery(t)
	if _, ok, err := run.ApplyAction(action); ok || !errors.Is(err, workflow.ErrSequenceExhausted) {
		t.Fatalf("overflow action: %t, %v", ok, err)
	}
	if run.State() != "square" || world.Snapshot() != before || len(records.Records()) != 0 {
		t.Fatal("overflow changed the run, graph or log")
	}
}
