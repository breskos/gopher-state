// Command acquaintance demonstrates saving and resuming a relationship workflow.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed relationship.json
var relationship []byte

type save struct {
	Base    workflow.Definition  `json:"base"`
	Current workflow.Definition  `json:"current"`
	Patches []workflow.Patch     `json:"patches"`
	Run     workflow.RunSnapshot `json:"run"`
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func demonstrate(out io.Writer) error {
	base, err := workflow.ReadJSON(bytes.NewReader(relationship), 8192)
	if err != nil {
		return err
	}
	world, err := workflow.NewWorld(base)
	if err != nil {
		return err
	}
	var log workflow.MemoryRecorder
	run, err := world.NewRun("mara-to-jo", nil, &log)
	if err != nil {
		return err
	}

	missing := run.Explain("talk", nil)
	fmt.Fprintf(out, "%s: %s (%s)\n", missing[0].Decision.Transition, missing[0].Failures[0].Kind, missing[0].Failures[0].Field)
	step := func(event string, day int, facts workflow.Facts) error {
		move, ok, err := run.StepWith(event, facts, workflow.StepOptions{
			Draw: func(uint64) uint64 { return 0 }, // A reproducible draw for this example.
			Meta: map[string]json.RawMessage{"day": json.RawMessage(fmt.Sprint(day))},
		})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("event %q did not advance from %q", event, run.State())
		}
		fmt.Fprintf(out, "Day %d: %s -> %s\n", day, move.From, move.To)
		return nil
	}
	if err := step("talk", 1, workflow.Facts{"meetings": workflow.Number(1)}); err != nil {
		return err
	}
	if err := step("talk", 5, workflow.Facts{"meetings": workflow.Number(3)}); err != nil {
		return err
	}
	if err := world.Add(workflow.Patch{Info: workflow.PatchInfo{ID: "community-event", Source: "simulation"}, Transitions: []workflow.Transition{
		{ID: "meet-at-community-event", From: "acquainted", To: "friend", Event: "community-event"},
	}}); err != nil {
		return err
	}

	data, err := json.Marshal(save{Base: base.Definition(), Current: world.Snapshot().Definition(), Patches: world.Patches(), Run: run.Snapshot()})
	if err != nil {
		return err
	}
	var loaded save
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	base, err = workflow.Compile(loaded.Base)
	if err != nil {
		return err
	}
	world, err = workflow.RestoreWorld(base, loaded.Patches)
	if err != nil {
		return err
	}
	run, err = world.ResumeRun(loaded.Run, nil, &log)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Restored at %s after %d transitions\n", run.State(), run.Snapshot().Sequence)

	if err := step("talk", 10, workflow.Facts{"trust": workflow.Number(0.7)}); err != nil {
		return err
	}
	if err := step("talk", 20, workflow.Facts{"trust": workflow.Number(0.9), "affection": workflow.Number(0.9)}); err != nil {
		return err
	}
	if err := step("review", 55, workflow.Facts{"daysWithoutContact": workflow.Quantity(35, "day")}); err != nil {
		return err
	}
	if err := world.Add(workflow.Patch{Info: workflow.PatchInfo{ID: "cancel-community-event"}, Retire: workflow.Retire{Transitions: []string{"meet-at-community-event"}}}); err != nil {
		return err
	}
	cancelled := world.Snapshot().Explain("acquainted", "community-event", nil)
	fmt.Fprintf(out, "%s: %s\n", cancelled[0].Decision.Transition, cancelled[0].Failures[0].Kind)
	return nil
}
