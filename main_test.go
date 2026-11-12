package main

import (
	"reflect"
	"testing"
)

func TestSplitKeys(t *testing.T) {
	for in, want := range map[string][]string{
		"q":            {"q"},
		"++":           {"+", "+"},
		"\x1b":         {"\x1b"},
		"\x1b[Cn":      {"\x1b[C", "n"},
		"\x1bOA\x1b[B": {"\x1bOA", "\x1b[B"},
		"\x1b[1;5C ":   {"\x1b[1;5C", " "},
		"\x1bx":        {"\x1bx"},
		"é\r":          {"é", "\r"},
		"\x1b[":        {"\x1b["},
		"\x1b[200~hi":  {"\x1b[200~", "h", "i"},
	} {
		if got := splitKeys([]byte(in)); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestQuoteArgs(t *testing.T) {
	for in, want := range map[string]string{
		"src":        "src",
		"go,ts":      "go,ts",
		"my dir":     "'my dir'",
		"a;b":        "'a;b'",
		"it's":       `'it'\''s'`,
		"~/code/*.x": "'~/code/*.x'",
	} {
		if got := quoteArgs([]string{in}); got != want {
			t.Errorf("%q: got %s, want %s", in, got, want)
		}
	}
}
