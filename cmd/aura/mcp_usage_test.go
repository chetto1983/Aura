package main

// mcp_usage_test.go pins the invariant that lets `aura mcp` print an error without the
// top-level usage banner. Split out of mcp_test.go on the 600-LOC cap (CLAUDE.md
// refactor-on-touch).

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestEveryMCPUsageErrorIsSelfContained is what makes runMCP's dropped banner safe. Each
// subcommand answers a wrong invocation with its OWN usage line, so appending the
// top-level one added nothing to a usage mistake and actively buried a real failure —
// a transport error arrived under a wall of subcommand syntax, telling the operator to
// check their spelling when the server was what had answered.
//
// If a future subcommand returns a bare "bad arguments" instead, this fails and the banner
// question has to be re-opened rather than silently regressing the help.
func TestEveryMCPUsageErrorIsSelfContained(t *testing.T) {
	cases := [][]string{
		{},
		{"recipes", "--json", "unexpected"},
		{"add"},
		{"install"},
		{"doctor"},
		{"tools"},
		{"enable"},
		{"disable"},
		{"remove"},
		{"login"},
		{"logout"},
		{"authorizations", "unexpected"},
	}
	for _, args := range cases {
		name := "no-subcommand"
		if len(args) > 0 {
			name = strings.Join(args, "_")
		}
		t.Run(name, func(t *testing.T) {
			err := runMCPCommand(context.Background(), nil, args, &bytes.Buffer{})
			if err == nil {
				t.Fatalf("args %v: want a usage error, got nil", args)
			}
			if !strings.HasPrefix(err.Error(), "usage: aura mcp") {
				t.Fatalf("args %v: error %q does not carry its own usage line", args, err)
			}
		})
	}
}
