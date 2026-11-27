package main

import (
	"bytes"
	"testing"
)

// TestGST600Acquaintance verifies the complete example in both halves of the run.
func TestGST600Acquaintance(t *testing.T) {
	var out bytes.Buffer
	if err := demonstrate(&out); err != nil {
		t.Fatal(err)
	}
	const want = `meet: missing (meetings)
Day 1: stranger -> familiar
Day 5: familiar -> acquainted
Restored at acquainted after 2 transitions
Day 10: acquainted -> friend
Day 20: friend -> close-friend
Day 55: close-friend -> friend
meet-at-community-event: retired
`
	if out.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}
