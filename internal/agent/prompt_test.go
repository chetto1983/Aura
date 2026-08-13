package agent

import (
	"regexp"
	"strings"
	"testing"
)

func TestPrompt_ProfileContextDoctrine(t *testing.T) {
	for _, needle := range []string{
		"long-term memory",
		"bounded current index",
		"<memory_context>",
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

// TestPrompt_MemoryDoctrine asserts automatic bounded recall, deep-recall fallback,
// proactive writes, and fail-soft posture without naming deferred memory tools.
func TestPrompt_MemoryDoctrine(t *testing.T) {
	for _, needle := range []string{
		"persistent long-term memory",
		"Read the supplied <memory_context>",
		"use deep memory recall before answering",
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
	// D-21: "the operator's language" means the language of the operator's most
	// recent message, which overrides a stored profile preference when the two
	// differ. This is the only language rule in the prompt (grep -c "operator's
	// language" == 1 is an acceptance criterion) — it is sharpened in place
	// rather than duplicated as a second line.
	if !strings.Contains(SystemPrompt, "most recent message") {
		t.Error(`system prompt no longer clarifies "the operator's language" as the language of the most recent message (D-21)`)
	}
	if strings.Count(SystemPrompt, "operator's language") != 1 {
		t.Errorf("system prompt must carry exactly ONE language rule, found %d occurrences of \"operator's language\"", strings.Count(SystemPrompt, "operator's language"))
	}
}

// TestPrompt_NoLeakedDeliberation asserts the D-21 rule that planning,
// self-critique and tool-selection reasoning never appear in a user-facing
// reply — the genuinely missing half of D-21 (the language half already existed
// and is pinned by TestPrompt_Directive above).
func TestPrompt_NoLeakedDeliberation(t *testing.T) {
	for _, needle := range []string{
		"self-critique",
		"working notes",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing the no-leaked-deliberation rule %q (D-21)", needle)
		}
	}
}

// The shell-call reduction doctrine is no longer asserted here. It did not go away
// — it moved into shell_exec's own Description, where it arrives WITH the schema at
// the moment a command is about to run, instead of thousands of tokens earlier in a
// prefix that also described a tool the model could not call. The invariant is
// asserted at its new home by
// tools.TestShellExecSpecCarriesItsOperationalRules.

// TestPrompt_NoTimestamp asserts the prompt contains no date/clock substring, so
// it can never poison the byte-stable cached prefix (Req#14 / D-08).
func TestPrompt_NoTimestamp(t *testing.T) {
	if loc := rfc3339ish.FindString(SystemPrompt); loc != "" {
		t.Errorf("system prompt contains a timestamp-shaped substring %q (cache-poisoning, D-08)", loc)
	}
}

// TestPrompt_NamesOnlyLoadedTools is the rule the old "mechanism, not enumeration"
// guard was reaching for, stated exactly.
//
// The old rule forbade naming volatile builtins, on the grounds that enumeration
// cache-busts the prefix. That reasoning was half right: what busts the cache is a
// name that CHANGES, and a tool's deferred-or-loaded status is a deliberate,
// committed decision, not per-turn drift. Meanwhile the rule permitted the failure
// that actually cost turns — the prompt taught shell_exec eight times and
// document_search three while both were deferred and uncallable, so the model was
// told in detail how to use tools that were not in its manifest.
//
// So: a tool may be named here if and only if it is LOADED. Everything deferred is
// referred to by capability family. Un-deferring a tool and forgetting to teach it
// is cheap; teaching one that is not there is what sent her to the public web for a
// customer code that was in the operator's own spreadsheet.
// The registry lives in package main, so the rule is enforced against the LIVE tool
// set by main.TestPromptNamesOnlyLoadedTools, which can build it. What is checked
// here is what needs no registry: the two structural verbs must stay explained, one
// being how she reaches everything deferred and the other the only way to end a turn.
func TestPrompt_NamesTheStructuralVerbs(t *testing.T) {
	for _, mechanism := range []string{"tool_search", "text_response"} {
		if !strings.Contains(SystemPrompt, mechanism) {
			t.Errorf("system prompt no longer explains %q", mechanism)
		}
	}
}

// TestPrompt_FullToolSurfaceDoctrine keeps Aura tool-aware without making her a
// coding-only agent.
//
// The two dropped phrases — "whole current tool list" and "most specific safe tool"
// — described a surface that did not exist: the current list held four tools that
// only ask, page, reply and search, so "scan the list for a dedicated tool" could
// only ever fail. The doctrine survives, addressed to what she can actually reach:
// pick the most specific CAPABILITY, and the families are enumerated so the choice
// is informed rather than a guess.
func TestPrompt_FullToolSurfaceDoctrine(t *testing.T) {
	for _, needle := range []string{
		"MOST SPECIFIC capability",
		"not only software engineering",
		"planning, task, or progress-tracking tool",
		"update it as each step completes",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt is missing full tool-surface doctrine %q", needle)
		}
	}
	// The families are the replacement for the enumeration the old wording assumed.
	// Without them "pick the most specific capability" is advice she cannot act on.
	for _, family := range []string{"filesystem —", "web —", "memory —", "documents —", "skills —", "delegation —"} {
		if !strings.Contains(SystemPrompt, family) {
			t.Errorf("system prompt no longer lists the %q capability family", family)
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
	// The install routing itself moved into the skill tool's own Description, which
	// is where the model reads it at the moment it is choosing between a skill and
	// hand-written code. It cannot stay here: the tool is deferred, so teaching it
	// in the prefix is teaching something absent from the manifest. The invariant is
	// asserted at its new home by tools.TestSkillDescriptionCarriesTheInstallOrder.
	if strings.Contains(SystemPrompt, "skill action=install") {
		t.Error("the install routing is back in the prompt; it belongs in the deferred tool's Description")
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
		// Both are LOADED now, so the doctrine may name them: the three-step flow is
		// useless if the model has to guess what performs steps 1 and 2.
		"document_search",
		"document_open",
		// document_index is deferred, so it is named by family instead — but the
		// doctrine it carries (a file you wrote is not findable until it is indexed)
		// must survive the rename.
		"documents capability",
		// The sharp clause, added after she searched the PUBLIC WEB on 2026-08-03 for
		// a customer code that was in the operator's own spreadsheet.
		"NEVER the public web",
	} {
		if !strings.Contains(SystemPrompt, needle) {
			t.Errorf("system prompt missing documents doctrine %q", needle)
		}
	}
}
