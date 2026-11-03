package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMailWatchShadowUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no subcommand":    nil,
		"an unknown one":   {"report"},
		"no identity":      {"shadow"},
		"an empty one":     {"shadow", "--identity="},
		"no days":          {"shadow", "--identity=11111111-1111-1111-1111-111111111111", "--days=0"},
		"days not a count": {"shadow", "--identity=11111111-1111-1111-1111-111111111111", "--days=week"},
	} {
		var out, errOut bytes.Buffer
		if code := mailWatchMain(context.Background(), args, &out, &errOut); code != exitUsage {
			t.Fatalf("%s: exit %d, want %d", name, code, exitUsage)
		}
		if !strings.Contains(errOut.String(), mailWatchUsage) || out.Len() != 0 {
			t.Fatalf("%s: out %q, err %q", name, out.String(), errOut.String())
		}
	}
}

func TestMailWatchShadowReadsItsFlags(t *testing.T) {
	identity, days, ok := parseMailWatchShadow([]string{"--identity", "11111111-1111-1111-1111-111111111111"})
	if !ok || identity != "11111111-1111-1111-1111-111111111111" || days != mailWatchShadowDays {
		t.Fatalf("parsed %q, %d, %v", identity, days, ok)
	}
	if _, days, ok := parseMailWatchShadow([]string{"--identity=x", "--days=7"}); !ok || days != 7 {
		t.Fatalf("days = %d, %v", days, ok)
	}
}
