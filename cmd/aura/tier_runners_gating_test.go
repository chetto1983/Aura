package main

import (
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/testledger"
)

func TestTierGatingTags(t *testing.T) {
	cases := map[string][]string{
		"a && !b":          {"a"},
		"a || b":           {"a", "b"},
		"!(a && !c)":       {"c"},
		"!web_integration": nil,
		"b && a && b":      {"a", "b"},
	}
	for src, want := range cases {
		expr, err := testledger.BuildConstraint([]byte("//go:build " + src + "\n\npackage x\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := gatingTags(expr); !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", src, got, want)
		}
	}
	if got := gatingTags(nil); got != nil {
		t.Errorf("no constraint gates nothing, got %v", got)
	}
}
