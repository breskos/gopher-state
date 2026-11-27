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

//go:embed testdata/resource-graph.json
var resourceGraph []byte

//go:embed testdata/resource-input.json
var resourceInput []byte

func ExampleRequirement() {
	engine, err := workflow.ReadJSON(bytes.NewReader(resourceGraph), 16<<10)
	if err != nil {
		fmt.Println(err)
		return
	}
	var input workflow.Input
	if err := json.Unmarshal(resourceInput, &input); err != nil {
		fmt.Println(err)
		return
	}
	decision, ok, err := engine.Step(engine.Start(), input.Event, input.Facts, func(uint64) uint64 { return 0 })
	fmt.Println(decision.Transition, ok, err)
	// Output: visit-market true <nil>
}

func TestResourceGraph(t *testing.T) {
	engine, err := workflow.ReadJSON(bytes.NewReader(resourceGraph), 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		field string
		value workflow.Value
		draw  uint64
		want  string
	}{
		{name: "all requirements met", want: "visit-market"},
		{name: "exact price is enough", field: "wallet", value: workflow.Quantity(250, "EUR-cent"), want: "visit-market"},
		{name: "insufficient money", field: "wallet", value: workflow.Quantity(249, "EUR-cent"), want: "work-from-home"},
		{name: "insufficient water", field: "water", value: workflow.Quantity(1, "L"), want: "work-from-home"},
		{name: "wrong resource unit", field: "water", value: workflow.Quantity(2000, "mL"), want: "work-from-home"},
		{name: "unit required", field: "water", value: workflow.Number(2), want: "work-from-home"},
		{name: "missing resource", field: "water", want: "work-from-home"},
		{name: "above distance limit", field: "walkingLimit", value: workflow.Quantity(299, "m"), want: "work-from-home"},
		{name: "distance units differ", field: "walkingLimit", value: workflow.Quantity(1, "km"), want: "work-from-home"},
		{name: "missing comparison operand", field: "walkingLimit", want: "work-from-home"},
		{name: "probability blocks selected transition", draw: (1 << 53) - 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var input workflow.Input
			if err := json.Unmarshal(resourceInput, &input); err != nil {
				t.Fatal(err)
			}
			if tt.field != "" {
				if tt.value.IsZero() {
					delete(input.Facts, tt.field)
				} else {
					input.Facts[tt.field] = tt.value
				}
			}
			draws := 0
			got, ok, err := engine.Step(engine.Start(), input.Event, input.Facts, func(limit uint64) uint64 {
				draws++
				if limit != 1<<53 {
					t.Fatalf("probability draw limit = %d", limit)
				}
				return tt.draw
			})
			if err != nil || ok != (tt.want != "") || got.Transition != tt.want {
				t.Fatalf("decision = %+v, %t, %v; want %q", got, ok, err, tt.want)
			}
			if (tt.want == "work-from-home" && draws != 0) || (tt.want != "work-from-home" && draws != 1) {
				t.Fatalf("unexpected draw count %d", draws)
			}
		})
	}
}

func TestWeightedSelectionThenProbability(t *testing.T) {
	const def = `{"version":1,"start":"a","states":["a"],"transitions":[
		{"id":"maybe","from":"a","to":"a","event":"go","selection":"weighted","weight":1,"probability":0.5},
		{"id":"certain","from":"a","to":"a","event":"go","selection":"weighted","weight":3},
		{"id":"never","from":"a","to":"a","event":"never","probability":0},
		{"id":"always","from":"a","to":"a","event":"always","probability":1}
	]}`
	engine, err := workflow.ReadJSON(strings.NewReader(def), 4096)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, ok, err := engine.Step("a", "go", nil, func(limit uint64) uint64 {
		calls++
		switch calls {
		case 1:
			if limit != 4 {
				t.Fatalf("weight draw limit = %d", limit)
			}
			return 0
		case 2:
			if limit != 1<<53 {
				t.Fatalf("probability draw limit = %d", limit)
			}
			return limit / 2
		default:
			t.Fatal("unexpected draw")
			return 0
		}
	})
	if got.Transition != "" || ok || err != nil || calls != 2 {
		t.Fatalf("weighted then rejected: %+v, %t, %v, %d", got, ok, err, calls)
	}
	for _, event := range []string{"never", "always"} {
		_, ok, err := engine.Step("a", event, nil, func(uint64) uint64 {
			t.Fatal("probability endpoint must not draw")
			return 0
		})
		if ok != (event == "always") || err != nil {
			t.Fatalf("%s: %t, %v", event, ok, err)
		}
	}
}

func TestInvalidRequirements(t *testing.T) {
	const prefix = `{"version":1,"start":"a","states":["a"],"transitions":[{"id":"go","from":"a","to":"a","event":"go",`
	for _, fields := range []string{
		`"probability":1.1`, `"probability":-0.1`,
		`"requires":[{"field":"cash","atLeast":true}]`,
		`"requires":[{"field":"cash","atLeast":-5}]`,
		`"requires":[{"field":"cash","atLeast":{"amount":5,"unit":""}}]`,
		`"requires":[{"field":"cash","atLeast":{"unit":"EUR-cent"}}]`,
		`"requires":[{"field":"cash","atLeast":{"amount":null,"unit":"EUR-cent"}}]`,
		`"when":{"field":"a","op":"gte","value":2,"otherField":"b"}`,
		`"when":{"field":"a","op":"exists","otherField":"b"}`,
	} {
		if _, err := workflow.ReadJSON(strings.NewReader(prefix+fields+"}]}"), 4096); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("invalid fields %s: %v", fields, err)
		}
	}
}
