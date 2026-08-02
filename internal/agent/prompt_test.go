package agent

import (
	"regexp"
	"strings"
	"testing"
)

func TestPrompt_ProfileContextDoctrine(t *testing.T) {
	for _, needle := range []string{
		"Recall it on demand",
		"long-term memory",
		"nothing pins it into your context",
		"current explicit instruction wins",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing profile-context doctrine %q", needle)
		}
	}
	// The profile is memory-backed now; the on-disk Agent.md path is superseded.
	if strings.Contains(SystemPrompt, "Agent.md") {
		t.Error("system prompt still references the deprecated Agent.md profile file")
	}
}

// TestPrompt_ProfileUsageRules asserts the relevance-gate, privacy, and
// language-override rules for memory-backed profile usage are present.
func TestPrompt_ProfileUsageRules(t *testing.T) {
	for _, want := range []string{
		"only when it is relevant",
		"infer or surface sensitive",
		"explicit in-message language request overrides",
	} {
		if !strings.Contains(SystemPrompt, want) {
			t.Errorf("system prompt missing profile usage rule %q", want)
		}
	}
}

// TestPrompt_MemoryDoctrine asserts the Phase-15 memory doctrine: D-01
// agent-decides writes (proactive, no confirmation), D-03 pull-on-demand recall
// (search before answering/asking), D-09 fail-soft posture — mechanism-level
// only, no volatile memory_* tool names (D-07 keeps them behind tool_search).
func TestPrompt_MemoryDoctrine(t *testing.T) {
	for _, needle := range []string{
		"persistent long-term memory",
		"pull-on-demand",
		"search memory BEFORE answering",
		"without being asked",
		"fail-soft",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing memory doctrine %q", needle)
		}
	}
	for _, volatile := range []string{"memory_search", "memory_add_entity", "memory__"} {
		if strings.Contains(SystemPrompt, volatile) {
			t.Errorf("system prompt enumerates memory tool surface %q (mechanism-not-enumeration, D-07)", volatile)
		}
	}
}

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

// TestPrompt_FullToolSurfaceDoctrine keeps Aura tool-aware without making it a
// coding-only agent or enumerating volatile tool names.
func TestPrompt_FullToolSurfaceDoctrine(t *testing.T) {
	for _, needle := range []string{
		"whole current tool list",
		"most specific safe tool",
		"not only software engineering",
		"planning, task, or progress-tracking tool",
		"update it as each step completes",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing full tool-surface doctrine %q", needle)
		}
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

// TestPrompt_WorkspaceDoctrine asserts the Amendment #88 <workspace> doctrine block:
// a fixed persistent /workspace working root, artifacts/ as the delivery staging
// dir, and the pre-baked toolchain no-reinstall directive.
func TestPrompt_WorkspaceDoctrine(t *testing.T) {
	for _, needle := range []string{"/workspace", "artifacts/", "already installed"} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt missing workspace doctrine %q", needle)
		}
	}
}

// TestPrompt_SkillInstallRoutingIsTheTool: the §Skills section must route INSTALL
// through the tool and must not send the model to the CLI for it.
//
// This guard used to assert the opposite — #51 had deleted action=install and handed
// self-extension to `npx skills add`. That was tested and wrong in production: the CLI
// installs into its working directory, which is not a loader root, so the model followed
// the instruction, read "Installation complete", and owned a skill Aura could not load.
// action=catalog stays superseded: discovery really is the CLI's job, because
// `npx skills find` only prints.
func TestPrompt_SkillInstallRoutingIsTheTool(t *testing.T) {
	if strings.Contains(SystemPrompt, "action=catalog") {
		t.Error("SystemPrompt reintroduced action=catalog (#51 removed it; discovery is the CLI)")
	}
	if !strings.Contains(SystemPrompt, "skill action=install") {
		t.Error("the §Skills section must route install through the tool, not the terminal")
	}
	if !strings.Contains(SystemPrompt, "NEVER install by running the skills CLI") {
		t.Error("the §Skills section must warn the model off the CLI install that cannot work")
	}
}

// TestPrompt_DocumentsDoctrine asserts the <documents> doctrine block: an uploaded
// document is not on the filesystem until it is fetched, document_search names the
// files, document_open writes one of them into /workspace, and a file the agent
// wrote becomes findable only once document_index records it.
func TestPrompt_DocumentsDoctrine(t *testing.T) {
	for _, needle := range []string{
		"<documents>",
		"NOT on the filesystem",
		"document_search",
		"document_open",
		"document_index",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt missing documents doctrine %q", needle)
		}
	}
}
