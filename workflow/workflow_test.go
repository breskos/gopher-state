package workflow_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed testdata/order.json
var order []byte

func ExampleReadJSON() {
	engine, err := workflow.ReadJSON(bytes.NewReader(order), 16<<10)
	if err != nil {
		fmt.Println(err)
		return
	}
	result, err := engine.Run([]workflow.Input{
		{Event: "submit", Facts: workflow.Facts{"risk": workflow.Number(25)}},
		{Event: "dispatch", Facts: workflow.Facts{"expressAvailable": workflow.Bool(true)}},
		{Event: "delivered"},
	}, func(limit uint64) uint64 { return 0 })
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, move := range result.Path {
		fmt.Println(move.Transition)
	}
	fmt.Println(result.State)
	// Output:
	// offer-shipping
	// pick-express
	// complete-express
	// done
}

func loadExample(t testing.TB) *workflow.Engine {
	t.Helper()
	m, err := workflow.ReadJSON(bytes.NewReader(order), int64(len(order)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSelection(t *testing.T) {
	m := loadExample(t)
	noDraw := func(uint64) uint64 {
		t.Fatal("unexpected random draw")
		return 0
	}
	for _, tt := range []struct {
		state, event, want string
		facts              workflow.Facts
	}{
		{"pending", "submit", "manual-review", workflow.Facts{"risk": workflow.Number(80)}},
		{"pending", "submit", "offer-shipping", workflow.Facts{"risk": workflow.Number(79)}},
		{"pending", "submit", "offer-shipping", workflow.Facts{"risk": workflow.String("80")}},
		{"standard", "dispatch", "pick-standard", nil},
		{"standard", "dispatch", "pick-standard", workflow.Facts{"expressAvailable": workflow.Bool(true), "region": workflow.String("remote")}},
	} {
		d, ok, err := m.Step(tt.state, tt.event, tt.facts, noDraw)
		if err != nil || !ok || d.Transition != tt.want {
			t.Errorf("Step(%s, %s, %v) = %+v, %t, %v", tt.state, tt.event, tt.facts, d, ok, err)
		}
	}

	facts := workflow.Facts{"expressAvailable": workflow.Bool(true)}
	for pick, want := range []string{"pick-express", "pick-standard", "pick-standard", "pick-standard"} {
		d, ok, err := m.Step("standard", "dispatch", facts, func(limit uint64) uint64 {
			if limit != 4 {
				t.Fatalf("draw limit = %d", limit)
			}
			return uint64(pick)
		})
		if err != nil || !ok || d.Transition != want {
			t.Errorf("weighted pick %d = %+v, %t, %v", pick, d, ok, err)
		}
	}
	if _, ok, err := m.Step("standard", "dispatch", facts, func(limit uint64) uint64 { return limit }); ok || !errors.Is(err, workflow.ErrInvalidDraw) {
		t.Fatalf("out-of-range draw: %t, %v", ok, err)
	}
	if _, ok, err := m.Step("missing", "submit", nil, nil); ok || !errors.Is(err, workflow.ErrUnknownState) {
		t.Fatalf("unknown state: %t, %v", ok, err)
	}

	result, err := m.Run([]workflow.Input{{Event: "submit"}, {Event: "unknown"}, {Event: "dispatch"}}, nil)
	if err != nil || result.State != "standard" || result.Consumed != 1 || len(result.Path) != 1 {
		t.Fatalf("blocked run = %+v, %v", result, err)
	}
	if allocations := testing.AllocsPerRun(50, func() { _, _, _ = m.Step("standard", "dispatch", facts, nil) }); allocations != 0 {
		t.Fatalf("Step allocated %.1f times per call", allocations)
	}
}

func TestConditionNullAndMissing(t *testing.T) {
	const doc = `{
		"version":1,"start":"a","states":["a","b"],
		"transitions":[{"id":"go","from":"a","to":"b","event":"go","when":{
			"all":[
				{"field":"approval","op":"exists"},
				{"any":[{"field":"approval","op":"eq","value":null},{"field":"score","op":"lt","value":10}]}
			]
		}}]
	}`
	m, err := workflow.ReadJSON(strings.NewReader(doc), 1024)
	if err != nil {
		t.Fatal(err)
	}
	for i, tt := range []struct {
		facts workflow.Facts
		want  bool
	}{
		{nil, false},
		{workflow.Facts{"approval": workflow.Null()}, true},
		{workflow.Facts{"approval": workflow.Bool(false), "score": workflow.Number(9)}, true},
		{workflow.Facts{"approval": workflow.Bool(false), "score": workflow.String("9")}, false},
		{workflow.Facts{"approval": workflow.Bool(false), "score": workflow.Number(math.NaN())}, false},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			if _, ok, err := m.Step("a", "go", tt.facts, nil); ok != tt.want || err != nil {
				t.Fatalf("Step = %t, %v", ok, err)
			}
		})
	}
}

func TestDefinitionValidation(t *testing.T) {
	const prefix = `{"version":1,"start":"a","states":["a","b"],"transitions":[`
	const edge = `{"id":"edge","from":"a","to":"b","event":"go"`
	for _, invalid := range []string{
		`{"version":2,"start":"a","states":["a"]}`,
		`{"version":1,"start":"a","states":["a","a"]}`,
		`{"version":1,"start":"c","states":["a","b"]}`,
		prefix + edge + `,"when":{"field":"a","op":"eq"}}]}`,
		prefix + edge + `,"when":{"field":"a","op":"eq","value":[]}}]}`,
		prefix + edge + `,"when":{"field":"a","op":"gte","value":"x"}}]}`,
		prefix + edge + `,"when":{"field":"a","op":"exists","value":null}}]}`,
		prefix + edge + `,"when":{"all":[]}}]}`,
		prefix + edge + `,"when":{"unknown":true}}]}`,
		prefix + edge + `,"when":{"all":[],"field":"a","op":"exists"}}]}`,
		prefix + edge + `,"selection":"weighted"}]}`,
		prefix + edge + `,"weight":1}]}`,
		prefix + edge + `},` + edge + `}]}`,
		prefix + edge + `}, {"id":"b","from":"a","to":"b","event":"go","selection":"weighted","weight":1}]}`,
		prefix + edge + `,"selection":"weighted","weight":18446744073709551615}, {"id":"b","from":"a","to":"b","event":"go","selection":"weighted","weight":1}]}`,
		prefix + edge + `}]}` + ` []`,
	} {
		if m, err := workflow.ReadJSON(strings.NewReader(invalid), 4096); m != nil || !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("ReadJSON(%s) = %v, %v", invalid, m, err)
		}
	}
	if _, err := workflow.ReadJSON(bytes.NewReader(order), int64(len(order)-1)); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("document limit: %v", err)
	}
	cycle := &workflow.Condition{}
	cycle.Not = cycle
	_, err := workflow.Compile(workflow.Definition{
		Version: 1, Start: "a", States: []string{"a"},
		Transitions: []workflow.Transition{{ID: "go", From: "a", To: "a", Event: "go", When: cycle}},
	})
	if !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("cycle: %v", err)
	}
}

func TestSnapshots(t *testing.T) {
	var def workflow.Definition
	if err := json.Unmarshal(order, &def); err != nil {
		t.Fatal(err)
	}
	m, err := workflow.Compile(def)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := m.Metadata("pick-express")
	if !ok {
		t.Fatal("missing metadata")
	}
	cost := string(meta["cost"])
	meta["cost"][0] = '!'
	def.Transitions[2].Metadata["cost"][0] = '!'
	def.Transitions[0].When.Value = workflow.Number(0)
	again, ok := m.Metadata("pick-express")
	if !ok || string(again["cost"]) != cost {
		t.Fatalf("metadata modified: %s", again["cost"])
	}
	d, ok, err := m.Step("pending", "submit", workflow.Facts{"risk": workflow.Number(1)}, nil)
	if err != nil || !ok || d.Transition != "offer-shipping" {
		t.Fatalf("compiled guard changed: %+v, %t, %v", d, ok, err)
	}
}

var resultSink workflow.Decision

func BenchmarkStep(b *testing.B) {
	m := loadExample(b)
	for _, tt := range []struct {
		name, state, event string
		facts              workflow.Facts
	}{
		{"First", "pending", "submit", workflow.Facts{"risk": workflow.Number(20)}},
		{"Weighted", "standard", "dispatch", workflow.Facts{"expressAvailable": workflow.Bool(true)}},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				resultSink, _, _ = m.Step(tt.state, tt.event, tt.facts, nil)
			}
		})
	}
}
