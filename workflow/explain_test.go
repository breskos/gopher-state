package workflow_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

func TestGST301Explain(t *testing.T) {
	const definition = `{"version":1,"start":"a","states":["a"],"transitions":[
		{"id":"needs-facts","from":"a","to":"a","event":"go",
		 "requires":[{"field":"wallet","atLeast":{"amount":10,"unit":"EUR-cent"}}],
		 "when":{"all":[
			{"any":[{"field":"x","op":"gte","value":2},{"field":"water","op":"gte","value":{"amount":2,"unit":"L"}}]},
			{"not":{"field":"blocked","op":"eq","value":true}},
			{"field":"attempt","op":"gt","otherField":"previous"}
		 ]}},
		{"id":"fallback","from":"a","to":"a","event":"go"},
		{"id":"disabled","from":"a","to":"a","event":"go","probability":0}
	]}`
	e, err := workflow.ReadJSON(strings.NewReader(definition), 4096)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workflow.NewWorld(e)
	if err != nil {
		t.Fatal(err)
	}
	run, err := w.NewRun("example", func(uint64) uint64 { t.Fatal("Explain drew randomness"); return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	facts := workflow.Facts{"wallet": workflow.Quantity(9, "EUR-cent"), "water": workflow.Quantity(2, "mL"), "blocked": workflow.Bool(true), "attempt": workflow.String("one"), "previous": workflow.String("two")}
	before := run.Snapshot()
	got := run.Explain("go", facts)
	if len(got) != 3 {
		t.Fatalf("candidates: %+v", got)
	}
	kinds, paths := []string{"requirement", "missing", "unit", "not", "type"}, []string{"requires[0]", "when.all[0].any[0]", "when.all[0].any[1]", "when.all[1].not", "when.all[2]"}
	if len(got[0].Failures) != len(kinds) {
		t.Fatalf("failures: %+v", got[0].Failures)
	}
	for i, f := range got[0].Failures {
		if f.Kind != kinds[i] || f.Path != paths[i] {
			t.Fatalf("failure %d: %+v", i, f)
		}
	}
	if got[2].Eligible || got[2].Failures[0].Kind != "probability" {
		t.Fatal("zero probability edge should not be offered")
	}
	matchOptions(t, got, run.Options("go", facts))
	if before != run.Snapshot() {
		t.Fatal("Explain advanced run")
	}
}

func matchOptions(t *testing.T, candidates []workflow.Candidate, options []workflow.Option) {
	t.Helper()
	var eligible []workflow.Option
	for _, c := range candidates {
		if c.Eligible {
			eligible = append(eligible, c.Option)
		}
	}
	if !reflect.DeepEqual(eligible, options) {
		t.Fatalf("eligible %v != options %v", eligible, options)
	}
}

func TestGST203StepOptionsAndResumedEncounter(t *testing.T) {
	var history workflow.MemoryRecorder
	s, err := newFestival(&history)
	if err != nil {
		t.Fatal(err)
	}
	publicEngine, err := workflow.ReadJSON(strings.NewReader(string(festival)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	public, err := workflow.NewWorld(publicEngine)
	if err != nil {
		t.Fatal(err)
	}
	people := make([]workflow.Turn, 2)
	bindings := make([]workflow.Binding, 2)
	modes := []workflow.Draw{func(uint64) uint64 { return 0 }, func(n uint64) uint64 { return n - 1 }}
	for i, name := range []string{"mia", "noah"} {
		mapping := map[string]string{"square": name + "-square", "stage": name + "-stage", "food-stall": name + "-food"}
		engine, err := workflow.Compile(workflow.Definition{Version: 1, Start: mapping["square"], States: []string{mapping["square"], mapping["stage"], mapping["food-stall"]}, Transitions: []workflow.Transition{
			{ID: "wait", From: mapping["square"], To: mapping["square"], Event: "wait"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		world, err := workflow.NewWorld(engine)
		if err != nil {
			t.Fatal(err)
		}
		r, err := world.ResumeRun(workflow.RunSnapshot{ID: name, State: mapping["square"], Sequence: 10}, modes[i], &history)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := r.StepTransitionWith("wait", "wait", nil, workflow.StepOptions{}); err != nil || !ok {
			t.Fatalf("resumed explicit step: %t %v", ok, err)
		}
		if _, ok, err := r.ApplyActionWith(workflow.Action{Event: "wait", Transition: "wait", Patch: workflow.Patch{States: []string{name + "-room"}}}, workflow.StepOptions{}); err != nil || !ok {
			t.Fatalf("resumed patch step: %t %v", ok, err)
		}
		bindings[i] = workflow.Binding{Run: r, States: mapping}
		people[i] = workflow.Turn{Run: r, Facts: tickets(1000)}
	}
	meeting, err := workflow.NewEncounter("shared", public, bindings)
	if err != nil {
		t.Fatal(err)
	}
	opt := workflow.StepOptions{Draw: func(uint64) uint64 { return 0 }, Meta: map[string]json.RawMessage{"tick": json.RawMessage("42")}}
	for _, person := range people {
		matchOptions(t, meeting.Explain(person.Run, "choose", person.Facts), meeting.Options(person.Run, "choose", person.Facts))
	}
	result, ok, err := meeting.TogetherWith("choose", "", people, opt, &history)
	if err != nil || !ok || result[0].Transition != "see-concert" || result[1].Transition != "see-concert" {
		t.Fatalf("together: %v %t %v", result, ok, err)
	}
	opt.Meta["tick"][0] = '9'
	for _, r := range history.Records()[4:] {
		if r.Sequence != 13 || string(r.Meta["tick"]) != "42" {
			t.Fatalf("group metadata: %+v", r)
		}
	}
	for _, r := range []*workflow.Run{s.mia, s.noah} {
		if _, ok, err := s.meeting.StepWith(r, workflow.Shared, "choose", "", tickets(1000), opt); err != nil || !ok {
			t.Fatalf("individual override: %t %v", ok, err)
		}
	}
	// Verify the same per-call draw overrides opposite defaults on plain runs too.
	for i, draw := range modes {
		world, err := workflow.NewWorld(publicEngine)
		if err != nil {
			t.Fatal(err)
		}
		r, err := world.NewRun(string(rune('a'+i)), draw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d, ok, err := r.StepWith("choose", tickets(1000), opt); err != nil || !ok || d.To != "stage" {
			t.Fatalf("draw override: %v %t %v", d, ok, err)
		}
	}
}

func TestGST003DefaultRunStepAllocations(t *testing.T) {
	e, err := workflow.ReadJSON(strings.NewReader(`{"version":1,"start":"a","states":["a"],"transitions":[{"id":"tick","from":"a","to":"a","event":"tick"}]}`), 1024)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workflow.NewWorld(e)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.NewRun("a", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testing.AllocsPerRun(100, func() {
		if _, ok, err := r.Step("tick", nil); err != nil || !ok {
			t.Fatalf("step: %t %v", ok, err)
		}
	}); got != 0 {
		t.Fatalf("default Step allocations = %g", got)
	}
}

func TestGST401SharedEdgeIntoRetiredLocalState(t *testing.T) {
	publicEngine, err := workflow.ReadJSON(strings.NewReader(string(festival)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	public, err := workflow.NewWorld(publicEngine)
	if err != nil {
		t.Fatal(err)
	}
	private, err := workflow.NewWorld(publicEngine)
	if err != nil {
		t.Fatal(err)
	}
	one, err := private.NewRun("one", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := public.NewRun("other", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{"square": "square", "stage": "stage", "food-stall": "food-stall"}
	enc, err := workflow.NewEncounter("group", public, []workflow.Binding{{Run: one, States: states}, {Run: other, States: states}})
	if err != nil {
		t.Fatal(err)
	}
	if err := private.Add(workflow.Patch{Retire: workflow.Retire{States: []string{"stage"}}}); err != nil {
		t.Fatal(err)
	}
	facts := tickets(650)
	candidates := enc.Explain(one, "choose", facts)
	matchOptions(t, candidates, enc.Options(one, "choose", facts))
	if candidates[2].Scope != workflow.Shared || candidates[2].Failures[0].Kind != "retired" {
		t.Fatalf("shared candidate: %+v", candidates[2])
	}
	if _, ok, err := enc.Step(one, workflow.Shared, "choose", "see-concert", facts); ok || err != nil {
		t.Fatalf("retired shared target: %t %v", ok, err)
	}
	if _, ok, err := enc.Together("choose", "see-concert", []workflow.Turn{{Run: one, Facts: facts}, {Run: other, Facts: facts}}, nil, nil); ok || err != nil {
		t.Fatalf("retired joint target: %t %v", ok, err)
	}
}
