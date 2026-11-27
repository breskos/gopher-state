package workflow_test

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

func TestValueJSONRoundTrip(t *testing.T) {
	for _, value := range []workflow.Value{
		workflow.Null(), workflow.Bool(true), workflow.Bool(false), workflow.Number(1.5),
		workflow.String("hello"), workflow.Quantity(2.5, "L"),
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded workflow.Value
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != value {
			t.Errorf("round trip %s: %v, %v", data, decoded, err)
		}
	}
	for _, value := range []workflow.Value{{}, workflow.Number(math.Inf(1)), workflow.Quantity(2, "")} {
		if _, err := json.Marshal(value); err == nil {
			t.Errorf("encoded invalid value %v", value)
		}
	}
	for _, invalid := range []string{`{"amount":1,"unit":"m","extra":1}`, `{"amount":1e999,"unit":"m"}`, `[]`} {
		var value workflow.Value
		if err := json.Unmarshal([]byte(invalid), &value); err == nil {
			t.Errorf("decoded invalid value %s", invalid)
		}
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadJSONErrors(t *testing.T) {
	if _, err := workflow.ReadJSON(strings.NewReader("{}"), 0); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("invalid byte limit: %v", err)
	}
	if _, err := workflow.ReadJSON(strings.NewReader("{\xff}"), 8); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("invalid UTF-8: %v", err)
	}
	failure := errors.New("reader failed")
	for _, reader := range []io.Reader{
		failingReader{failure}, io.MultiReader(strings.NewReader("{}"), failingReader{failure}),
	} {
		if _, err := workflow.ReadJSON(reader, 2); !errors.Is(err, failure) {
			t.Fatalf("reader error was not preserved: %v", err)
		}
	}
}

func TestWorldRejectsInvalidOperations(t *testing.T) {
	if _, err := workflow.NewWorld(&workflow.Engine{}); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("uncompiled world: %v", err)
	}
	var empty workflow.World
	if err := empty.Add(workflow.Patch{}); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("uninitialized world patch: %v", err)
	}
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.NewRun("", nil, nil); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("empty run ID: %v", err)
	}
	before := world.Snapshot()
	for _, invalid := range []string{`null`, `{"states":["home"]}`, `{"other":[]}`} {
		if err := world.AddJSON(strings.NewReader(invalid), 128); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("invalid patch %s: %v", invalid, err)
		}
	}
	if err := world.AddJSON(strings.NewReader("{}"), 2); err != nil || world.Snapshot() != before {
		t.Fatalf("empty patch should preserve snapshot: %v", err)
	}
}
