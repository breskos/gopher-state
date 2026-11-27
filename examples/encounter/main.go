// Command encounter demonstrates private choices, a shared scene and joint moves.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed festival.json
var festival []byte

type person struct {
	ID      string              `json:"id"`
	Facts   workflow.Facts      `json:"facts"`
	Private workflow.Definition `json:"private"`
	Binding map[string]string   `json:"binding"`
}

type scene struct {
	Public workflow.Definition `json:"public"`
	People []person            `json:"people"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("encounter", flag.ContinueOnError)
	flags.SetOutput(out)
	individual := flags.Bool("individual", false, "make independent choices instead of a joint choice")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var config scene
	if err := json.Unmarshal(festival, &config); err != nil {
		return err
	}
	public, err := compileWorld(config.Public)
	if err != nil {
		return err
	}

	// Each person has their own world and run. Only the bindings below expose
	// any of the public graph to them.
	history := &workflow.MemoryRecorder{}
	var bindings []workflow.Binding
	var turns []workflow.Turn
	for _, p := range config.People {
		private, err := compileWorld(p.Private)
		if err != nil {
			return err
		}
		run, err := private.NewRun(p.ID, nil, history)
		if err != nil {
			return err
		}
		bindings = append(bindings, workflow.Binding{Run: run, States: p.Binding})
		turns = append(turns, workflow.Turn{Run: run, Facts: p.Facts})
	}
	meeting, err := workflow.NewEncounter("festival", public, bindings)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Festival open")
	for i, turn := range turns {
		fmt.Fprintf(out, "%s's choices:\n", config.People[i].ID)
		for _, option := range meeting.Options(turn.Run, "choose", turn.Facts) {
			fmt.Fprintf(out, "  %s: %s -> %s\n", option.Scope, option.Decision.Transition, option.Decision.To)
		}
	}

	if *individual {
		fmt.Fprintln(out, "Independent choices")
		for i, choice := range []struct {
			scope workflow.Scope
			id    string
		}{{workflow.Private, "visit-library"}, {workflow.Shared, "visit-food-stall"}} {
			turn := turns[i]
			if _, ok, err := meeting.Step(turn.Run, choice.scope, "choose", choice.id, turn.Facts); err != nil || !ok {
				return fmt.Errorf("individual choice %q: advanced=%t, error=%v", choice.id, ok, err)
			}
		}
	} else {
		fmt.Fprintln(out, "Joint choice: only the food stall is eligible for both people")
		if _, ok, err := meeting.Together("choose", "", turns, nil, history); err != nil || !ok {
			return fmt.Errorf("joint choice: advanced=%t, error=%v", ok, err)
		}
	}

	meeting.Close()
	fmt.Fprintln(out, "Festival closed")
	for _, record := range history.Records() {
		d := record.Decision
		fmt.Fprintf(out, "%s #%d: %s --%s--> %s\n", record.RunID, record.Sequence, d.From, d.Transition, d.To)
	}
	return nil
}

func compileWorld(def workflow.Definition) (*workflow.World, error) {
	engine, err := workflow.Compile(def)
	if err != nil {
		return nil, err
	}
	return workflow.NewWorld(engine)
}
