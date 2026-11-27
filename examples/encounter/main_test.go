package main

import (
	"bytes"
	"errors"
	"flag"
	"testing"
)

const choices = `Festival open
Mia's choices:
  private: visit-library -> library
  shared: see-concert -> mia-concert
  shared: visit-food-stall -> mia-food
Noah's choices:
  private: visit-bike-shop -> bike-shop
  shared: visit-food-stall -> noah-food
`

func TestCLI(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		tail string
	}{
		{"joint", nil, `Joint choice: only the food stall is eligible for both people
Festival closed
Mia #1: mia-meeting-point --visit-food-stall--> mia-food
Noah #1: noah-meeting-point --visit-food-stall--> noah-food
`},
		{"individual", []string{"-individual"}, `Independent choices
Festival closed
Mia #1: mia-meeting-point --visit-library--> library
Noah #1: noah-meeting-point --visit-food-stall--> noah-food
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(tt.args, &out); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), choices+tt.tail; got != want {
				t.Errorf("output:\n%s\nwant:\n%s", got, want)
			}
		})
	}
	var out bytes.Buffer
	if err := run([]string{"-h"}, &out); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help returned %v", err)
	}
}
