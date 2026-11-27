package workflow_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

func ExampleGraph_WriteDOT() {
	engine, err := workflow.Compile(workflow.Definition{
		Version: 1, Start: "idle", States: []string{"idle", "active"},
		Transitions: []workflow.Transition{{ID: "start", From: "idle", To: "active", Event: "go"}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = engine.Graph().WriteDOT(io.Discard)
	fmt.Println("DOT written")
	// Output: DOT written
}

// TestGST501DOT validates stable ordering, escaping, provenance-independent
// labels and effective retirement as presented to a visualization.
func TestGST501DOT(t *testing.T) {
	engine, err := workflow.Compile(workflow.Definition{
		Version: 1, Start: "a\"\\b", States: []string{"a\"\\b", "end\nstate"},
		RetiredStates: []string{"end\nstate"},
		Transitions:   []workflow.Transition{{ID: "edge", From: "a\"\\b", To: "end\nstate", Event: "go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if err := engine.Graph().WriteDOT(&first); err != nil {
		t.Fatal(err)
	}
	if err := engine.Graph().WriteDOT(&second); err != nil || first.String() != second.String() {
		t.Fatal("DOT output changed between calls")
	}
	for _, fragment := range []string{`label="a\"\\b"`, `label="end\nstate", style=dashed`, `label="go [edge]", style=dashed`, "start -> n0"} {
		if !strings.Contains(first.String(), fragment) {
			t.Errorf("missing %q in %s", fragment, first.String())
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestGST501DOTErrors(t *testing.T) {
	if err := (workflow.Graph{}).WriteDOT(io.Discard); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("missing start: %v", err)
	}
	graph := workflow.Graph{Start: "a", Nodes: []workflow.GraphNode{{ID: "a"}, {ID: "a"}}}
	if err := graph.WriteDOT(io.Discard); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("duplicate node: %v", err)
	}
	graph.Nodes = graph.Nodes[:1]
	graph.Edges = []workflow.GraphEdge{{Transition: workflow.Transition{ID: "bad", From: "a", To: "missing"}}}
	if err := graph.WriteDOT(io.Discard); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("missing edge endpoint: %v", err)
	}
	graph.Edges = nil
	if err := graph.WriteDOT(brokenWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writer error: %v", err)
	}
}
