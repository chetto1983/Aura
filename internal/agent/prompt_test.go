package agent

import (
	"regexp"
	"strings"
	"testing"
)

// rfc3339ish matches a date or clock-shaped substring that would poison the
// cached prefix (Req#14 / D-08). Any hit means a timestamp crept into the prompt.
var rfc3339ish = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}`)

// TestPrompt_Directive asserts the system prompt carries the explicit User Language
// output directive (Req#14 prompt half; feedback_all_prompts_in_english_only).
func TestPrompt_Directive(t *testing.T) {
	if !strings.Contains(SystemPrompt, "Always respond in User Language") {
		t.Error(`system prompt is missing the "Always respond in User Language" directive`)
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
