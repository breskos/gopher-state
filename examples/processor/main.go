// Command processor demonstrates how an external policy grows a workflow graph.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/breskos/gopher-state/workflow"
)

const maxDocumentBytes = 16 << 10

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: processor BASE_GRAPH.json ACTION.json")
	}
	base, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("open base graph: %w", err)
	}
	defer base.Close()
	action, err := os.Open(args[1])
	if err != nil {
		return fmt.Errorf("open action: %w", err)
	}
	defer action.Close()
	return simulate(base, action, out)
}

func simulate(base, action io.Reader, out io.Writer) error {
	engine, err := workflow.ReadJSON(base, maxDocumentBytes)
	if err != nil {
		return err
	}
	world, err := workflow.NewWorld(engine)
	if err != nil {
		return err
	}
	person, err := world.NewRun("explorer", nil, nil)
	if err != nil {
		return err
	}
	if _, ok, err := person.Step("walk", nil); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("the base graph has no eligible walk from %q", person.State())
	}

	// The processor supplies the JSON action and its updated facts.
	discovery, ok, err := person.ApplyActionJSON(action, maxDocumentBytes)
	if err != nil {
		return err
	}
	if !ok {
		_, err := fmt.Fprintf(out, "Discovery rejected; explorer remains at %s\n", person.State())
		return err
	}
	if _, err := fmt.Fprintf(out, "Discovery: %s -> %s\n", discovery.From, discovery.To); err != nil {
		return err
	}

	// The policy can deliberately prefer an option with a lower graph weight.
	// The engine still checks that the choice is eligible at execution time.
	facts := workflow.Facts{}
	chosen := ""
	for _, option := range person.Options("explore", facts) {
		if _, err := fmt.Fprintf(out, "Option: %s (weight %d)\n", option.Decision.Transition, option.Weight); err != nil {
			return err
		}
		if option.Decision.To == "fountain" {
			chosen = option.Decision.Transition
		}
	}
	if chosen == "" {
		return fmt.Errorf("processor found no eligible path to the fountain")
	}
	move, ok, err := person.StepTransition("explore", chosen, facts)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("processor choice %q did not advance the run", chosen)
	}
	_, err = fmt.Fprintf(out, "Processor chose %s; explorer is now at %s\n", move.Transition, move.To)
	return err
}
