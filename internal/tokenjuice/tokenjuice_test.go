package tokenjuice_test

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/tokenjuice"
)

func TestLoadBuiltinRulesCount(t *testing.T) {
	rules := tokenjuice.LoadBuiltinRules()
	if len(rules) < 10 {
		t.Errorf("LoadBuiltinRules: want ≥10 rules, got %d", len(rules))
	}
}

func TestStructuredAuraToolPayloadsPassThrough(t *testing.T) {
	stdout := `{"path":"workspace/TOOLS.md","type":"file","bytes":640,"total_bytes":640,"truncated":false,"encoding":"utf-8","content":"# TOOLS\n` + strings.Repeat("payload line\n", 80) + `LOAD_BEARING_SENTINEL"}`

	for _, toolName := range []string{"file", "web", "skill", "source", "search"} {
		t.Run(toolName, func(t *testing.T) {
			r := tokenjuice.Compact(tokenjuice.Input{
				ToolName: toolName,
				Argv:     []string{"read"},
				Stdout:   stdout,
			}, tokenjuice.Options{})
			if r.Applied {
				t.Fatalf("structured payload: Applied=true, RuleID=%q", r.RuleID)
			}
			if r.RuleID != "none/structured-tool" {
				t.Fatalf("structured payload: RuleID=%q, want none/structured-tool", r.RuleID)
			}
			if r.InlineText != stdout {
				t.Fatal("structured payload was altered")
			}
			if !strings.Contains(r.InlineText, "LOAD_BEARING_SENTINEL") {
				t.Fatal("structured payload lost sentinel content")
			}
		})
	}
}

func TestCompactPassthrough(t *testing.T) {
	in := tokenjuice.Input{Stdout: "hello"}
	r := tokenjuice.Compact(in, tokenjuice.Options{})
	if r.Applied {
		t.Errorf("Applied: want false, got true")
	}
	if r.InlineText != "hello" {
		t.Errorf("InlineText: want %q, got %q", "hello", r.InlineText)
	}
}
