package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rfc3339ish matches a date or clock-shaped substring that would poison the
// cached prefix (Req#14 / D-08). Any hit means a timestamp crept into the prompt.
var rfc3339ish = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}`)

// TestPrompt_Directive asserts the system prompt carries the explicit output-language
// directive (Req#14 prompt half; feedback_all_prompts_in_english_only). The needle
// follows the operator-authored 2026-06-06 rewrite ("the operator's language"
// superseded the older "User Language" phrasing — same intent, new authored wording).
func TestPrompt_Directive(t *testing.T) {
	if !strings.Contains(SystemPrompt, "Always respond in the operator's language") {
		t.Error(`system prompt is missing the "Always respond in the operator's language" directive`)
	}
}

func TestPrompt_ShellTransactionDoctrine(t *testing.T) {
	for _, needle := range []string{"shell transaction", "exit_code/cwd/duration", "separate calls for pwd or exit-code checks"} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing shell-call reduction doctrine %q", needle)
		}
	}
}

// TestPrompt_NoTimestamp asserts the prompt contains no date/clock substring, so
// it can never poison the byte-stable cached prefix (Req#14 / D-08).
func TestPrompt_NoTimestamp(t *testing.T) {
	if loc := rfc3339ish.FindString(SystemPrompt); loc != "" {
		t.Errorf("system prompt contains a timestamp-shaped substring %q (cache-poisoning, D-08)", loc)
	}
}

// TestPrompt_MechanismNotEnumeration asserts the prompt names the discovery
// MECHANISM (tool_search, text_response) but does not enumerate the volatile
// builtins by name — enumeration cache-busts the prefix when the tool set changes
// (D-09). tool_search and text_response are stable contract surfaces, allowed.
func TestPrompt_MechanismNotEnumeration(t *testing.T) {
	for _, volatile := range []string{"read_tool_output", "current_time"} {
		if strings.Contains(SystemPrompt, volatile) {
			t.Errorf("system prompt enumerates the volatile tool %q (cache-busts the prefix, D-09)", volatile)
		}
	}
	if !strings.Contains(SystemPrompt, "tool_search") {
		t.Error("system prompt must explain the tool_search discovery mechanism")
	}
	if !strings.Contains(SystemPrompt, "text_response") {
		t.Error("system prompt must explain the text_response termination mechanism")
	}
}

// TestPrompt_ByteStable asserts two reads of the prompt are byte-identical — the
// seed assertion for prefix stability across turns (Req#14). systemMessage() must
// read no clock and take no per-turn input.
func TestPrompt_ByteStable(t *testing.T) {
	first := systemMessage()
	second := systemMessage()
	if first != second {
		t.Error("systemMessage() is not byte-stable across reads")
	}
	if first != SystemPrompt {
		t.Error("systemMessage() diverged from the SystemPrompt constant")
	}
}

// TestPrompt_DocSyncByteIdentical asserts the SystemPrompt const and its authored
// source of truth docs/system_prompt.txt are byte-identical (the txt carries a single
// trailing newline the Go const does not). The two MUST be edited together — this test
// is the enforcement the doc comment promises (amendment #51 / D-40 sync).
func TestPrompt_DocSyncByteIdentical(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "system_prompt.txt"))
	if err != nil {
		t.Fatalf("read docs/system_prompt.txt: %v", err)
	}
	doc := strings.TrimRight(string(raw), "\n")
	if doc != SystemPrompt {
		t.Errorf("docs/system_prompt.txt diverged from the SystemPrompt const — edit both together.\n"+
			"len(doc)=%d len(const)=%d", len(doc), len(SystemPrompt))
	}
}

// TestPrompt_NoSupersededSkillRouting is the #51 supersession guard: the §Skills
// section must NOT teach the deleted action=catalog/action=install routing (those
// tool actions are gone; discovery+install is the always-on find-skills skill). A
// future edit cannot reintroduce the dead teaching without tripping this — in BOTH
// the const and the authored doc.
func TestPrompt_NoSupersededSkillRouting(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "system_prompt.txt"))
	if err != nil {
		t.Fatalf("read docs/system_prompt.txt: %v", err)
	}
	for _, superseded := range []string{"action=catalog", "action=install"} {
		if strings.Contains(SystemPrompt, superseded) {
			t.Errorf("SystemPrompt reintroduced superseded routing %q (#51 removed it)", superseded)
		}
		if strings.Contains(string(raw), superseded) {
			t.Errorf("docs/system_prompt.txt reintroduced superseded routing %q (#51 removed it)", superseded)
		}
	}
	// The shrunk section must point at the always-on discovery skill.
	if !strings.Contains(SystemPrompt, "always-on") {
		t.Error("the §Skills section must name an always-on skill as the discovery/install teaching")
	}
}
