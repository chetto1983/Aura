package tokenjuice_test

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/tokenjuice"
)

func TestLoadBuiltinRulesCount(t *testing.T) {
	rules := tokenjuice.LoadBuiltinRules()
	if len(rules) < 13 {
		t.Errorf("LoadBuiltinRules: want ≥13 rules, got %d", len(rules))
	}
}

func TestAuraFileReadUnder500Chars(t *testing.T) {
	// Verify the aura/file-read rule reduces a large file-read JSON to <500 chars.
	// Input is a realistic file tool output > 512 bytes.
	stdout := `{"path":"workspace/TOOLS.md","type":"file","bytes":640,"total_bytes":640,"truncated":false,"encoding":"utf-8","content":"# TOOLS\nThis document describes all available tools.\n\n## file\nRead, write, list, search, or patch files.\n\n## web\nFetch URLs and search the web.\n\n## wiki_page\nCreate and update wiki pages.\n\n## source\nStore, OCR, and ingest document sources.\n\n## schedule_task\nSchedule recurring or one-time tasks.\n\n## search_memory\nSearch the wiki knowledge base.\n\n## execute_shell\nRun shell commands in the sandbox.\n\n## execute_code\nRun Python code in the sandbox.\n"}`
	in := tokenjuice.Input{
		ToolName: "file",
		Argv:     []string{"read"},
		Stdout:   stdout,
	}
	r := tokenjuice.Compact(in, tokenjuice.Options{})
	if !r.Applied {
		t.Fatalf("aura/file-read: expected Applied=true, got false (RuleID=%q)", r.RuleID)
	}
	if r.RuleID != "aura/file-read" {
		t.Errorf("aura/file-read: expected RuleID=aura/file-read, got %q", r.RuleID)
	}
	if len(r.InlineText) >= 500 {
		t.Errorf("aura/file-read: expected output <500 chars, got %d: %q", len(r.InlineText), r.InlineText)
	}
	if strings.Contains(r.InlineText, `"content":`) {
		t.Errorf("aura/file-read: content field should be dropped from output")
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
