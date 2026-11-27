package workflow_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/breskos/gopher-state/workflow"
)

//go:embed testdata/live-base.json
var liveBase []byte

//go:embed testdata/live-market.json
var livePatch []byte

func loadWorld() (*workflow.World, error) {
	engine, err := workflow.ReadJSON(bytes.NewReader(liveBase), 4096)
	if err != nil {
		return nil, err
	}
	return workflow.NewWorld(engine)
}

func startAtSquare(world *workflow.World, id string, recorder workflow.Recorder) (*workflow.Run, error) {
	run, err := world.NewRun(id, func(uint64) uint64 { return 0 }, recorder)
	if err != nil {
		return nil, err
	}
	if _, ok, err := run.Step("walk", nil); err != nil || !ok {
		return nil, fmt.Errorf("walk: accepted=%t, error=%v", ok, err)
	}
	return run, nil
}

func ExampleWorld_AddJSON() {
	world, err := loadWorld()
	if err != nil {
		fmt.Println(err)
		return
	}
	var history workflow.MemoryRecorder
	person, err := startAtSquare(world, "person-1", &history)
	if err != nil {
		fmt.Println(err)
		return
	}

	// A pop-up market appears while the person is already on the square.
	if err := world.AddJSON(bytes.NewReader(livePatch), 4096); err != nil {
		fmt.Println(err)
		return
	}
	_, _, err = person.Step("explore", workflow.Facts{
		"wallet": workflow.Quantity(400, "EUR-cent"), "marketOpen": workflow.Bool(true),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, record := range history.Records() {
		fmt.Println(record.RunID, record.Sequence, record.Decision.From, record.Decision.Transition, record.Decision.To)
	}
	// Output:
	// person-1 1 home walk-to-square square
	// person-1 2 square visit-pop-up pop-up-market
}

func TestLiveWorldAndRecording(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	base := world.Snapshot()
	var history workflow.MemoryRecorder
	alex, err := startAtSquare(world, "alex", &history)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := startAtSquare(world, "bob", &history)
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := startAtSquare(world, "quiet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := alex.Step("explore", nil); ok || err != nil {
		t.Fatalf("explore before patch: %t, %v", ok, err)
	}
	if err := world.AddJSON(bytes.NewReader(livePatch), 4096); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := base.Step("square", "explore", nil, nil); ok || err != nil {
		t.Fatalf("old snapshot changed: %t, %v", ok, err)
	}

	facts := workflow.Facts{"wallet": workflow.Quantity(400, "EUR-cent"), "marketOpen": workflow.Bool(true), "unrelated": workflow.String("private")}
	for _, tt := range []struct {
		run   *workflow.Run
		facts workflow.Facts
		want  string
	}{{alex, facts, "pop-up-market"}, {bob, nil, "market-cafe"}, {quiet, nil, "market-cafe"}} {
		d, ok, err := tt.run.Step("explore", tt.facts)
		if err != nil || !ok || d.To != tt.want || tt.run.State() != tt.want {
			t.Fatalf("explore: %+v, %t, %v, state=%s", d, ok, err, tt.run.State())
		}
	}
	facts["wallet"] = workflow.Quantity(0, "EUR-cent")
	records := history.Records()
	if len(records) != 4 {
		t.Fatalf("records = %d", len(records))
	}
	got := records[2]
	if got.RunID != "alex" || got.Sequence != 2 || got.Weight != 3 ||
		got.Probability != 0.8 || got.Selection != workflow.Weighted ||
		got.Condition == nil || len(got.Condition.All) != 2 ||
		len(got.Facts) != 2 || got.Facts["wallet"] != workflow.Quantity(400, "EUR-cent") {
		t.Fatalf("unexpected record: %+v", got)
	}
	if records[3].RunID != "bob" || records[3].Sequence != 2 {
		t.Fatalf("unexpected other run record: %+v", records[3])
	}
	got.Facts["wallet"] = workflow.Quantity(1, "EUR-cent")
	got.Condition.All[0].Field = "changed"
	again := history.Records()[2]
	if again.Facts["wallet"] != workflow.Quantity(400, "EUR-cent") || again.Condition.All[0].Field != "wallet" {
		t.Fatal("mutating a returned record affected recorder storage")
	}
}

func TestWorldPatchAtomicAndConcurrent(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	before := world.Snapshot()
	invalid := `{"states":["temp"],"transitions":[
		{"id":"valid","from":"home","to":"temp","event":"walk"},
		{"id":"invalid","from":"temp","to":"unknown","event":"walk"}
	]}`
	if err := world.AddJSON(strings.NewReader(invalid), 4096); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("invalid patch: %v", err)
	}
	if world.Snapshot() != before {
		t.Fatal("invalid patch published a snapshot")
	}
	for i := range 12 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			id := fmt.Sprintf("shop-%d", i)
			if err := world.Add(workflow.Patch{
				States:      []string{id},
				Transitions: []workflow.Transition{{ID: id, From: "square", To: id, Event: id}},
			}); err != nil {
				t.Fatal(err)
			}
			r, err := startAtSquare(world, id, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := r.Step(id, nil); !ok || err != nil || r.State() != id {
				t.Fatalf("live step failed: %t, %v, %s", ok, err, r.State())
			}
		})
	}
}

func TestPersistedRecordAndRecorderFailure(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startAtSquare(world, "saved", workflow.NewJSONLRecorder(file)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record workflow.Record
	if err := json.Unmarshal(data, &record); err != nil || record.RunID != "saved" || record.Decision.To != "square" || !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("persisted record = %+v, %v", record, err)
	}
	fail := true
	run, err := world.NewRun("retry", nil, workflow.RecorderFunc(func(record workflow.Record) error {
		if record.Sequence != 1 {
			t.Fatalf("failed write consumed a sequence number: %d", record.Sequence)
		}
		if fail {
			return io.ErrShortWrite
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := run.Step("walk", nil); ok || !errors.Is(err, io.ErrShortWrite) || run.State() != "home" {
		t.Fatalf("failed recorder advanced state: %t, %v, %s", ok, err, run.State())
	}
	fail = false
	if _, ok, err := run.Step("walk", nil); !ok || err != nil || run.State() != "square" {
		t.Fatalf("retry failed: %t, %v, %s", ok, err, run.State())
	}
}

func TestParallelRunsShareJSONLRecorder(t *testing.T) {
	world, err := loadWorld()
	if err != nil {
		t.Fatal(err)
	}
	if err := world.Add(workflow.Patch{Transitions: []workflow.Transition{
		{ID: "wait-at-home", From: "home", To: "home", Event: "wait"},
	}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	recorder := workflow.NewJSONLRecorder(&out)
	const people, steps = 8, 15
	t.Run("runs", func(t *testing.T) {
		for i := range people {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				run, err := world.NewRun(fmt.Sprint(i), nil, recorder)
				if err != nil {
					t.Fatal(err)
				}
				for range steps {
					if _, ok, err := run.Step("wait", nil); !ok || err != nil {
						t.Fatalf("wait: %t, %v", ok, err)
					}
				}
			})
		}
	})
	seen := make(map[string]uint64)
	decoder := json.NewDecoder(&out)
	for {
		var record workflow.Record
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		seen[record.RunID]++
		if record.Sequence != seen[record.RunID] || record.Decision.Transition != "wait-at-home" {
			t.Fatalf("unexpected record: %+v", record)
		}
	}
	if len(seen) != people {
		t.Fatalf("recorded %d people, want %d", len(seen), people)
	}
	for run, count := range seen {
		if count != steps {
			t.Errorf("run %s has %d records, want %d", run, count, steps)
		}
	}
}
