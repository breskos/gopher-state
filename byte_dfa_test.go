package gopherstate_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	state "github.com/breskos/gopher-state"
)

func TestByteDFA(t *testing.T) {
	def := byteGraph(4, 2)
	sparse, err := state.CompileDFA(def)
	if err != nil {
		t.Fatal(err)
	}
	// Two equivalent explicit columns and one implicit dead column.
	dense, err := state.CompileByteDFA(def, def.StateCount*2*4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.CompileByteDFA(def, def.StateCount*2*4-1); !errors.Is(err, state.ErrTableLimit) {
		t.Fatalf("transition budget: %v", err)
	}
	for from := range def.StateCount {
		for value := range 256 {
			want, exists := sparse.Step(from, byte(value))
			if target, ok := dense.Step(from, byte(value)); target != want || ok != exists {
				t.Fatalf("Step(%d, %d) = %d, %t; want %d, %t", from, value, target, ok, want, exists)
			}
		}
	}
	for _, input := range [][]byte{nil, {0, 1, 0}, {0, 1, 0, 0}, {2}, {1, 255}} {
		if dense.Match(input) != sparse.Match(input) || dense.Analyze(input, state.Observer{}) != sparse.Analyze(input, state.Observer{}) {
			t.Fatalf("byte table disagrees with sparse DFA for %v", input)
		}
	}
}

func TestByteDFAAllColumnsDistinct(t *testing.T) {
	def := state.Definition[byte]{StateCount: 256, Accepting: []int{255}}
	for value := range 256 {
		def.Transitions = append(def.Transitions, state.Transition[byte]{Symbol: byte(value), To: value})
	}
	dense, err := state.CompileByteDFA(def, 256*256*4)
	if err != nil {
		t.Fatal(err)
	}
	for value := range 256 {
		if target, ok := dense.Step(0, byte(value)); !ok || target != value {
			t.Errorf("Step(0, %d) = %d, %t", value, target, ok)
		}
	}
	if !dense.Match([]byte{255}) || dense.Match([]byte{254}) || dense.Match([]byte{255, 255}) {
		t.Fatal("unexpected acceptance at the end of the transition table")
	}
}

func byteGraph(states, alphabet int) state.Definition[byte] {
	def := state.Definition[byte]{StateCount: states, Accepting: []int{states - 1}}
	for from := range states {
		for value := range alphabet {
			def.Transitions = append(def.Transitions, state.Transition[byte]{
				From: from, Symbol: byte(value), To: (from + value/2 + 1) % states,
			})
		}
	}
	return def
}

func BenchmarkByteDFA(b *testing.B) {
	for _, alphabet := range []int{1, 32, 256} {
		def := byteGraph(64, alphabet)
		sparse, err := state.CompileDFA(def)
		if err != nil {
			b.Fatal(err)
		}
		dense, err := state.CompileByteDFA(def, 1<<20)
		if err != nil {
			b.Fatal(err)
		}
		for _, length := range []int{16, 4096} {
			input := bytes.Repeat([]byte{byte(alphabet - 1)}, length)
			for _, variant := range []struct {
				name  string
				match func([]byte) bool
			}{{"Sparse", sparse.Match}, {"Table", dense.Match}} {
				b.Run(fmt.Sprintf("alphabet=%d/length=%d/%s", alphabet, length, variant.name), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						benchmarkAccepted = variant.match(input)
					}
				})
			}
		}
	}
}

func BenchmarkCompileByteDFA(b *testing.B) {
	for _, alphabet := range []int{1, 32, 256} {
		def := byteGraph(64, alphabet)
		b.Run(fmt.Sprintf("alphabet=%d/Sparse", alphabet), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := state.CompileDFA(def); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("alphabet=%d/Table", alphabet), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := state.CompileByteDFA(def, 1<<20); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
