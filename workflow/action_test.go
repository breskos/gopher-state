package workflow_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed testdata/discover-action.json
var discovery []byte

func ExampleRun_ApplyActionJSON() {
	world, err := loadWorld()
	if err != nil {
		fmt.Println(err)
		return
	}
	person, err := startAtSquare(world, "explorer", nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	// An application processor produces this action after discovering the garden.
	decision, ok, err := person.ApplyActionJSON(bytes.NewReader(discovery), 4096)
	if err != nil || !ok {
		fmt.Println(ok, err)
		return
	}
	fmt.Println(decision.From, "->", decision.To)
	for _, option := range person.Options("explore", nil) {
		fmt.Println(option.Decision.Transition, option.Weight)
	}
	decision, _, err = person.StepTransition("explore", "visit-fountain", nil)
	fmt.Println(decision.To, err)
	// Output:
	// square -> hidden-garden
	// visit-greenhouse 3
	// visit-fountain 1
	// fountain <nil>
}

func decodeDiscovery(t *testing.T) workflow.Action {
	t.Helper()
	var action workflow.Action
	if err := json.Unmarshal(discovery, &action); err != nil {
		t.Fatal(err)
	}
	return action
}

func TestProcessorActionPublishesWholePatch(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	old := world.Snapshot()
	var history workflow.MemoryRecorder
	run, err := startAtSquare(world, "creator", &history)
	if err != nil {
		t.Fatal(err)
	}
	other, err := startAtSquare(world, "observer", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := other.Options("discover", workflow.Facts{"hasKey": workflow.Bool(true)}); len(got) != 0 {
		t.Fatalf("premature options: %v", got)
	}

	action := decodeDiscovery(t)
	d, ok, err := run.ApplyAction(action)
	if err != nil || !ok || d.To != "hidden-garden" || run.State() != d.To {
		t.Fatalf("apply discovery: %+v, %t, %v", d, ok, err)
	}
	if world.Snapshot() == old || len(old.Options("square", "discover", action.Facts)) != 0 {
		t.Fatal("old graph was changed or new graph was not published")
	}
	if got := other.Options("discover", action.Facts); len(got) != 1 || got[0].Decision.To != "hidden-garden" {
		t.Fatalf("observer options: %v", got)
	}
	if _, ok, err := other.StepTransition("discover", d.Transition, nil); ok || err != nil || other.State() != "square" {
		t.Fatalf("stale eligibility accepted: %t, %v", ok, err)
	}
	if got := run.Options("explore", nil); len(got) != 2 || got[0].Weight != 3 || got[1].Weight != 1 {
		t.Fatalf("new branches: %v", got)
	}
	if d, ok, err := run.StepTransition("explore", "visit-fountain", nil); !ok || err != nil || d.To != "fountain" {
		t.Fatalf("explicit selection: %+v, %t, %v", d, ok, err)
	}
	records := history.Records()
	if len(records) != 3 || records[1].Decision.To != "hidden-garden" || records[2].Sequence != 3 {
		t.Fatalf("processor history: %v", records)
	}
}

func TestProcessorActionRollback(t *testing.T) {
	writeFailed := errors.New("cannot record action")
	for _, tc := range []struct {
		name   string
		change func(*workflow.Action)
		fail   bool
		err    error
	}{
		{"missing guard fact", func(a *workflow.Action) { a.Facts = nil }, false, nil},
		{"unknown transition", func(a *workflow.Action) { a.Transition = "not-a-transition" }, false, nil},
		{"zero probability", func(a *workflow.Action) { zero := 0.0; a.Patch.Transitions[0].Probability = &zero }, false, nil},
		{"invalid generated edge", func(a *workflow.Action) { a.Patch.Transitions[2].To = "unknown" }, false, workflow.ErrInvalidDefinition},
		{"recorder failure", func(*workflow.Action) {}, true, writeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, err := loadWorld()
			if err != nil {
				t.Fatal(err)
			}
			base := world.Snapshot()
			recorded := 0
			run, err := startAtSquare(world, "creator", workflow.RecorderFunc(func(r workflow.Record) error {
				if r.Decision.Event == "discover" && tc.fail {
					return writeFailed
				}
				recorded++
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			action := decodeDiscovery(t)
			tc.change(&action)
			if _, ok, err := run.ApplyAction(action); ok || !errors.Is(err, tc.err) {
				t.Fatalf("action: %t, %v; want %v", ok, err, tc.err)
			}
			if world.Snapshot() != base || run.State() != "square" || recorded != 1 {
				t.Fatal("rejected action changed the world, run or log")
			}
		})
	}
}

func TestActionInputs(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	run, err := world.NewRun("creator", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := run.ApplyAction(workflow.Action{Event: "walk"}); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("missing transition ID: %v", err)
	}
	for _, doc := range []string{"{}", "null", `{"unexpected":true}`} {
		if _, _, err := run.ApplyActionJSON(strings.NewReader(doc), 1024); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("invalid action %s: %v", doc, err)
		}
	}
	if _, _, err := run.ApplyActionJSON(bytes.NewReader(discovery), 20); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("action byte limit: %v", err)
	}
	if _, ok, err := run.ApplyAction(workflow.Action{Event: "walk", Transition: "walk-to-square"}); err != nil || !ok {
		t.Fatalf("ordinary action without a patch: %t, %v", ok, err)
	}
}

func TestSimultaneousProcessorActions(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("actions", func(t *testing.T) {
		for i := range 8 {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				id := fmt.Sprintf("destination-%d", i)
				run, err := startAtSquare(world, id, nil)
				if err != nil {
					t.Fatal(err)
				}
				action := workflow.Action{
					Event: "discover", Transition: id,
					Patch: workflow.Patch{States: []string{id}, Transitions: []workflow.Transition{
						{ID: id, From: "square", To: id, Event: "discover"},
					}},
				}
				if d, ok, err := run.ApplyAction(action); err != nil || !ok || d.To != id {
					t.Fatalf("concurrent action: %+v, %t, %v", d, ok, err)
				}
			})
		}
	})
	if got := world.Snapshot().Options("square", "discover", nil); len(got) != 8 {
		t.Fatalf("lost generated options: %v", got)
	}
}
