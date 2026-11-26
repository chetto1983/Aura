package main

import (
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/testledger"
)

func TestTierGatingTags(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{"a negated tag gates nothing", "a && !b", []string{"a"}},
		{"either side of an or gates", "a || b", []string{"a", "b"}},
		{"a double negation gates", "!(a && !c)", []string{"c"}},
		{"only negated", "!web_integration", nil},
		{"sorted and once", "b && a && b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := testledger.BuildConstraint([]byte("//go:build " + tc.src + "\n\npackage x\n"))
			if err != nil {
				t.Fatal(err)
			}
			if got := gatingTags(expr); !slices.Equal(got, tc.want) {
				t.Errorf("%s: got %v, want %v", tc.src, got, tc.want)
			}
		})
	}
	if got := gatingTags(nil); got != nil {
		t.Errorf("no constraint gates nothing, got %v", got)
	}
}
