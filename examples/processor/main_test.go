package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const fixtureDir = "../../workflow/testdata/"

func TestProcessorCommand(t *testing.T) {
	base := fixtureDir + "live-base.json"
	action := fixtureDir + "discover-action.json"
	var out bytes.Buffer
	if err := run([]string{base, action}, &out); err != nil {
		t.Fatal(err)
	}
	const want = `Discovery: square -> hidden-garden
Option: visit-greenhouse (weight 3)
Option: visit-fountain (weight 1)
Processor chose visit-fountain; explorer is now at fountain
`
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProcessorCommandRejectedAction(t *testing.T) {
	data, err := os.ReadFile(fixtureDir + "discover-action.json")
	if err != nil {
		t.Fatal(err)
	}
	blocked := bytes.Replace(data, []byte(`"hasKey": true`), []byte(`"hasKey": false`), 1)
	if bytes.Equal(data, blocked) {
		t.Fatal("test did not remove the key")
	}
	path := filepath.Join(t.TempDir(), "blocked.json")
	if err := os.WriteFile(path, blocked, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{fixtureDir + "live-base.json", path}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "Discovery rejected; explorer remains at square\n" {
		t.Errorf("unexpected rejection output: %q", got)
	}
}
