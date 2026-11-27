package workflow_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed testdata/shared-festival.json
var festival []byte

type festivalScenario struct {
	meeting   *workflow.Encounter
	mia, noah *workflow.Run
}

func newFestival(recorder workflow.Recorder) (festivalScenario, error) {
	engine, err := workflow.ReadJSON(bytes.NewReader(festival), 4096)
	if err != nil {
		return festivalScenario{}, err
	}
	public, err := workflow.NewWorld(engine)
	if err != nil {
		return festivalScenario{}, err
	}
	newPerson := func(name, privateChoice string) (*workflow.Run, map[string]string, error) {
		states := map[string]string{
			"square": name + "-meeting-point", "stage": name + "-concert", "food-stall": name + "-food",
		}
		engine, err := workflow.Compile(workflow.Definition{
			Version: 1, Start: states["square"],
			States: []string{states["square"], states["stage"], states["food-stall"], privateChoice},
			Transitions: []workflow.Transition{
				{ID: privateChoice, From: states["square"], To: privateChoice, Event: "choose"},
			},
		})
		if err != nil {
			return nil, nil, err
		}
		world, err := workflow.NewWorld(engine)
		if err != nil {
			return nil, nil, err
		}
		run, err := world.NewRun(name, func(uint64) uint64 { return 0 }, recorder)
		return run, states, err
	}
	mia, miaStates, err := newPerson("mia", "visit-library")
	if err != nil {
		return festivalScenario{}, err
	}
	noah, noahStates, err := newPerson("noah", "visit-bike-shop")
	if err != nil {
		return festivalScenario{}, err
	}
	meeting, err := workflow.NewEncounter("festival", public, []workflow.Binding{
		{Run: mia, States: miaStates}, {Run: noah, States: noahStates},
	})
	return festivalScenario{meeting, mia, noah}, err
}

func tickets(amount float64) workflow.Facts {
	return workflow.Facts{"wallet": workflow.Quantity(amount, "EUR-cent")}
}

func describeChoices(options []workflow.Option) []string {
	var result []string
	for _, option := range options {
		result = append(result, string(option.Scope)+":"+option.Decision.Transition)
	}
	return result
}

func ExampleEncounter() {
	scenario, err := newFestival(nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(describeChoices(scenario.meeting.Options(scenario.mia, "choose", tickets(600))))
	fmt.Println(describeChoices(scenario.meeting.Options(scenario.noah, "choose", tickets(300))))

	decisions, ok, err := scenario.meeting.Together("choose", "", []workflow.Turn{
		{Run: scenario.mia, Facts: tickets(600)}, {Run: scenario.noah, Facts: tickets(300)},
	}, nil, nil)
	if err != nil || !ok {
		fmt.Println(ok, err)
		return
	}
	fmt.Println(decisions[0].Transition, decisions[0].To, decisions[1].To)
	// Output:
	// [private:visit-library shared:see-concert shared:visit-food-stall]
	// [private:visit-bike-shop shared:visit-food-stall]
	// visit-food-stall mia-food noah-food
}

func TestEncounterPrivateChoiceAndVisibility(t *testing.T) {
	s, err := newFestival(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := describeChoices(s.meeting.Options(s.mia, "choose", tickets(600))); !slices.Equal(got, []string{
		"private:visit-library", "shared:see-concert", "shared:visit-food-stall",
	}) {
		t.Fatalf("Mia's options = %v", got)
	}
	if _, ok, err := s.meeting.Step(s.noah, workflow.Private, "choose", "visit-library", nil); ok || err != nil {
		t.Fatalf("other person's private option: %t, %v", ok, err)
	}
	if _, ok, err := s.meeting.Step(s.mia, workflow.Private, "choose", "visit-library", nil); !ok || err != nil {
		t.Fatalf("private option: %t, %v", ok, err)
	}
	if s.noah.State() != "noah-meeting-point" || len(s.meeting.Options(s.mia, "choose", tickets(600))) != 0 {
		t.Fatal("private move affected the other run or left shared options visible")
	}
	s.meeting.Close()
	got := describeChoices(s.meeting.Options(s.noah, "choose", tickets(600)))
	if !slices.Equal(got, []string{"private:visit-bike-shop"}) {
		t.Fatalf("options after close = %v", got)
	}
	if _, ok, err := s.meeting.Step(s.noah, workflow.Shared, "choose", "visit-food-stall", nil); ok || err != nil {
		t.Fatalf("shared move after close: %t, %v", ok, err)
	}
	if _, ok, err := s.meeting.Step(s.noah, workflow.Private, "choose", "", nil); !ok || err != nil {
		t.Fatalf("private move after close: %t, %v", ok, err)
	}
}

type failedGroup struct{ err error }

func (f failedGroup) WriteGroup([]workflow.Record) error { return f.err }

func TestEncounterJointAction(t *testing.T) {
	var memory workflow.MemoryRecorder
	s, err := newFestival(&memory)
	if err != nil {
		t.Fatal(err)
	}
	turns := []workflow.Turn{{Run: s.noah, Facts: tickets(300)}, {Run: s.mia, Facts: tickets(600)}}
	if _, ok, err := s.meeting.Together("choose", "see-concert", turns, nil, &memory); ok || err != nil {
		t.Fatalf("one person cannot afford concert: %t, %v", ok, err)
	}
	turns[0].Facts = tickets(600)
	if _, ok, err := s.meeting.Together("choose", "see-concert", turns, func(limit uint64) uint64 { return limit - 1 }, &memory); ok || err != nil {
		t.Fatalf("probability rejected group: %t, %v", ok, err)
	}

	fail := errors.New("cannot write group")
	if _, ok, err := s.meeting.Together("choose", "see-concert", turns, func(uint64) uint64 { return 0 }, failedGroup{fail}); ok || !errors.Is(err, fail) {
		t.Fatalf("failed joint write: %t, %v", ok, err)
	}
	if s.mia.State() != "mia-meeting-point" || s.noah.State() != "noah-meeting-point" || len(memory.Records()) != 0 {
		t.Fatal("unsuccessful joint action changed a state or log")
	}

	calls := 0
	decisions, ok, err := s.meeting.Together("choose", "", turns, func(limit uint64) uint64 {
		calls++
		if (calls == 1 && limit != 4) || (calls == 2 && limit != 1<<53) {
			t.Fatalf("unexpected draw %d with limit %d", calls, limit)
		}
		return 0
	}, &memory)
	if err != nil || !ok || calls != 2 || decisions[0].To != "noah-concert" || decisions[1].To != "mia-concert" {
		t.Fatalf("joint concert: %v, %t, %v, draws=%d", decisions, ok, err, calls)
	}
	records := memory.Records()
	if len(records) != 2 {
		t.Fatalf("joint records = %d", len(records))
	}
	wantIDs := []string{"noah", "mia"}
	for i, r := range records {
		if r.RunID != wantIDs[i] || r.Sequence != 1 || r.Encounter != "festival" || r.Shared == nil ||
			r.Shared.From != "square" || r.Shared.To != "stage" || r.Decision != decisions[i] {
			t.Fatalf("unexpected joint record %+v", r)
		}
	}
	var out bytes.Buffer
	if err := workflow.NewJSONLRecorder(&out).WriteGroup(records); err != nil {
		t.Fatal(err)
	}
	var row struct{ Group []workflow.Record }
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || len(row.Group) != 2 || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("invalid JSONL group: %s, %v", out.Bytes(), err)
	}
}

func TestEncounterSharedChoiceIsIndividual(t *testing.T) {
	var memory workflow.MemoryRecorder
	s, err := newFestival(&memory)
	if err != nil {
		t.Fatal(err)
	}
	move, ok, err := s.meeting.Step(s.mia, workflow.Shared, "choose", "see-concert", tickets(600))
	if err != nil || !ok || move.To != "mia-concert" || s.noah.State() != "noah-meeting-point" {
		t.Fatalf("individual shared move = %+v, %t, %v", move, ok, err)
	}
	if records := memory.Records(); len(records) != 1 || records[0].Shared == nil || records[0].Shared.To != "stage" {
		t.Fatalf("unexpected log: %+v", records)
	}
}

func TestConcurrentJointActionsAdvanceOnce(t *testing.T) {
	var memory workflow.MemoryRecorder
	s, err := newFestival(&memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("competing actions", func(t *testing.T) {
		for i, participants := range [][]workflow.Turn{
			{{Run: s.mia}, {Run: s.noah}},
			{{Run: s.noah}, {Run: s.mia}},
		} {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				if _, _, err := s.meeting.Together("choose", "visit-food-stall", participants, nil, &memory); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
	if s.mia.State() != "mia-food" || s.noah.State() != "noah-food" || len(memory.Records()) != 2 {
		t.Fatal("competing joint actions should advance and log each person once")
	}
}

func TestEncounterRejectsInvalidBindings(t *testing.T) {
	s, err := newFestival(nil)
	if err != nil {
		t.Fatal(err)
	}
	public, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	valid := workflow.Binding{Run: s.mia, States: map[string]string{"square": "mia-meeting-point"}}
	for _, tt := range []struct {
		id       string
		world    *workflow.World
		bindings []workflow.Binding
	}{
		{"", public, []workflow.Binding{valid}},
		{"meeting", nil, []workflow.Binding{valid}},
		{"meeting", public, nil},
		{"meeting", public, []workflow.Binding{{}}},
		{"meeting", public, []workflow.Binding{valid, valid}},
		{"meeting", public, []workflow.Binding{{Run: s.mia, States: map[string]string{"bad": "mia-meeting-point"}}}},
		{"meeting", public, []workflow.Binding{{Run: s.mia, States: map[string]string{"square": "bad"}}}},
		{"meeting", public, []workflow.Binding{{Run: s.mia, States: map[string]string{"square": "mia-meeting-point", "home": "mia-meeting-point"}}}},
	} {
		if _, err := workflow.NewEncounter(tt.id, tt.world, tt.bindings); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("NewEncounter(%q, %v) = %v", tt.id, tt.bindings, err)
		}
	}
}

func TestEncounterRejectsInvalidMoves(t *testing.T) {
	var memory workflow.MemoryRecorder
	s, err := newFestival(&memory)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.meeting.Options(nil, "choose", nil); len(got) != 0 {
		t.Fatalf("nil run options = %v", got)
	}
	if _, _, err := s.meeting.Step(nil, workflow.Shared, "choose", "", nil); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("nil run step: %v", err)
	}
	if _, _, err := s.meeting.Step(s.mia, "invalid", "choose", "", nil); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("invalid scope: %v", err)
	}
	turn := workflow.Turn{Run: s.mia}
	for _, inputs := range [][]workflow.Turn{nil, {turn, turn}, {turn, {}}} {
		if _, _, err := s.meeting.Together("choose", "", inputs, nil, &memory); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Errorf("invalid participants %v: %v", inputs, err)
		}
	}
	both := []workflow.Turn{turn, {Run: s.noah}}
	if _, _, err := s.meeting.Together("choose", "", both, nil, nil); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("missing group recorder: %v", err)
	}
	if _, ok, err := s.meeting.Step(s.mia, workflow.Shared, "choose", "", tickets(600)); !ok || err != nil {
		t.Fatalf("shared move before split: %t, %v", ok, err)
	}
	if _, ok, err := s.meeting.Together("choose", "", both, nil, &memory); ok || err != nil {
		t.Fatalf("joint action at different positions: %t, %v", ok, err)
	}
	s.meeting.Close()
	if _, ok, err := s.meeting.Together("choose", "", both, nil, &memory); ok || err != nil {
		t.Fatalf("joint action after closure: %t, %v", ok, err)
	}
}

func TestEncounterPartialBinding(t *testing.T) {
	s, err := newFestival(nil)
	if err != nil {
		t.Fatal(err)
	}
	publicEngine, err := workflow.ReadJSON(bytes.NewReader(festival), 4096)
	if err != nil {
		t.Fatal(err)
	}
	public, err := workflow.NewWorld(publicEngine)
	if err != nil {
		t.Fatal(err)
	}
	mapping := map[string]string{"square": "mia-meeting-point", "food-stall": "mia-food"}
	meeting, err := workflow.NewEncounter("food-only", public, []workflow.Binding{{Run: s.mia, States: mapping}})
	if err != nil {
		t.Fatal(err)
	}
	// The constructor snapshots the application-supplied bindings.
	mapping["stage"] = "mia-concert"
	if got := describeChoices(meeting.Options(s.mia, "choose", tickets(600))); !slices.Equal(got, []string{
		"private:visit-library", "shared:visit-food-stall",
	}) {
		t.Fatalf("partially mapped choices = %v", got)
	}
	if got := describeChoices(meeting.Options(s.noah, "choose", tickets(600))); !slices.Equal(got, []string{"private:visit-bike-shop"}) {
		t.Fatalf("unenrolled person's choices = %v", got)
	}
	if _, ok, err := meeting.Step(s.mia, workflow.Shared, "choose", "see-concert", tickets(600)); ok || err != nil {
		t.Fatalf("unmapped target: %t, %v", ok, err)
	}
}

func TestEncounterInvalidRandomDrawDoesNotAdvance(t *testing.T) {
	s, err := newFestival(nil)
	if err != nil {
		t.Fatal(err)
	}
	turns := []workflow.Turn{{Run: s.mia, Facts: tickets(600)}, {Run: s.noah, Facts: tickets(600)}}
	for _, explicitID := range []string{"", "see-concert"} {
		if _, ok, err := s.meeting.Together("choose", explicitID, turns, func(limit uint64) uint64 {
			return limit
		}, nil); ok || !errors.Is(err, workflow.ErrInvalidDraw) {
			t.Fatalf("invalid draw for %q: %t, %v", explicitID, ok, err)
		}
	}
	if s.mia.State() != "mia-meeting-point" || s.noah.State() != "noah-meeting-point" {
		t.Fatal("invalid random draw advanced a participant")
	}
}

func FuzzEncounterJointSelection(f *testing.F) {
	f.Add(uint16(499), uint16(500), uint8(0), true)
	f.Add(uint16(500), uint16(500), uint8(0), false)
	f.Add(uint16(500), uint16(500), uint8(2), true)
	f.Add(uint16(700), uint16(900), uint8(3), true)
	f.Fuzz(func(t *testing.T, miaWallet, noahWallet uint16, pick uint8, concertRuns bool) {
		s, err := newFestival(nil)
		if err != nil {
			t.Fatal(err)
		}
		turns := []workflow.Turn{
			{Run: s.mia, Facts: tickets(float64(miaWallet))},
			{Run: s.noah, Facts: tickets(float64(noahWallet))},
		}
		decisions, ok, err := s.meeting.Together("choose", "", turns, func(limit uint64) uint64 {
			switch limit {
			case 4:
				return uint64(pick % 4)
			case 1 << 53:
				if concertRuns {
					return 0
				}
				return limit - 1
			default:
				t.Fatalf("unexpected draw limit %d", limit)
				return 0
			}
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		concertSelected := miaWallet >= 500 && noahWallet >= 500 && pick%4 < 3
		if concertSelected && !concertRuns {
			if ok || len(decisions) != 0 || s.mia.State() != "mia-meeting-point" || s.noah.State() != "noah-meeting-point" {
				t.Fatalf("rejected concert advanced the group: %v, %t", decisions, ok)
			}
			return
		}
		want := "visit-food-stall"
		if concertSelected {
			want = "see-concert"
		}
		if !ok || len(decisions) != 2 || decisions[0].Transition != want || decisions[1].Transition != want {
			t.Fatalf("joint action = %v, %t; want both runs to take %q", decisions, ok, want)
		}
	})
}
