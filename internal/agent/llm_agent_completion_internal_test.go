package agent

// White-box tests for the completion-gate helpers that the black-box suite cannot
// reach: the verdict parser (NOT_DONE must win over the DONE substring), the
// reason extractor, the nudge-skipping request finder, and the side-effect digest.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestParseCriticVerdict(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantDone bool
		wantOK   bool
	}{
		{"plain_done", "DONE", true, true},
		{"lower_done", "done", true, true},
		{"done_with_tail", "DONE - the file exists at /tmp/x", true, true},
		{"not_done_colon", "NOT_DONE: the xlsx was never produced", false, true},
		{"not_done_lower", "not_done: missing file", false, true},
		{"not_done_space", "NOT DONE: nothing ran", false, true},
		{"not_done_wins_over_done_substring", "NOT_DONE: still not DONE yet", false, true},
		{"prose_not_done", "The task is not_done because no file exists", false, true},
		{"unparseable", "maybe, hard to say", false, false},
		{"empty", "   ", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done, _, ok := parseCriticVerdict(tc.in)
			if done != tc.wantDone || ok != tc.wantOK {
				t.Errorf("parseCriticVerdict(%q) = (done=%v, ok=%v), want (done=%v, ok=%v)",
					tc.in, done, ok, tc.wantDone, tc.wantOK)
			}
		})
	}
}

func TestParseCriticVerdict_ExtractsReason(t *testing.T) {
	_, reason, _ := parseCriticVerdict("NOT_DONE: run create.py and read it back\n(second line ignored)")
	if reason != "run create.py and read it back" {
		t.Errorf("reason = %q, want the first line after the token", reason)
	}
}

func TestExtractReason(t *testing.T) {
	cases := map[string]string{
		": the file is missing": "the file is missing",
		" - run it now":         "run it now",
		"  no separator at all": "no separator at all",
		": first\nsecond":       "first",
	}
	for in, want := range cases {
		if got := extractReason(in); got != want {
			t.Errorf("extractReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastUserRequest_SkipsAgentNudges(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "build me the spreadsheet"},
		{Role: llm.RoleAssistant, Content: "ok"},
		{Role: llm.RoleUser, Content: recoveryNudgeGeneric},
		{Role: llm.RoleUser, Content: completionVetoPrefix + "no file"},
	}
	if got := lastUserRequest(history); got != "build me the spreadsheet" {
		t.Errorf("lastUserRequest = %q, want the genuine request (nudges skipped)", got)
	}
}

func TestSideEffectDigest_IncludesToolsExcludesTerminal(t *testing.T) {
	a, _ := synthAgent("s")
	a.history = append(a.history,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "w1", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "fs_write", Arguments: `{"path":"/tmp/x.py"}`}},
		}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "w1", Content: "wrote 10 bytes to /tmp/x.py"},
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "t1", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: terminalTool, Arguments: `{"text":"all set"}`}},
		}},
	)
	got := a.sideEffectDigest()
	if !strings.Contains(got, "fs_write(") || !strings.Contains(got, "/tmp/x.py") {
		t.Errorf("digest missing the fs_write call: %q", got)
	}
	if !strings.Contains(got, "wrote 10 bytes") {
		t.Errorf("digest missing the tool result: %q", got)
	}
	if strings.Contains(got, terminalTool) {
		t.Errorf("digest must exclude the terminal tool, got: %q", got)
	}
}

func TestSideEffectDigest_PrefersLatestVerificationEvidence(t *testing.T) {
	a, _ := synthAgent("s")
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("noise-%02d", i)
		tc := llm.ToolCall{ID: id, Type: "function"}
		tc.Function.Name = "skill"
		tc.Function.Arguments = `{"action":"info","name":"very-large-skill"}`
		a.history = append(a.history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{tc}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: strings.Repeat("early setup noise ", 80)},
		)
	}
	verify := llm.ToolCall{ID: "verify", Type: "function"}
	verify.Function.Name = "shell_exec"
	verify.Function.Arguments = `{"command":"python verify_xlsx.py"}`
	a.history = append(a.history,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{verify}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "verify", Content: "openpyxl OK yahoo_market_oggi.xlsx contains 2026-06-09"},
	)

	got := a.sideEffectDigest()
	if !strings.Contains(got, "openpyxl OK yahoo_market_oggi.xlsx") {
		t.Fatalf("digest omitted latest verification evidence: %q", got)
	}
	if strings.Contains(got, "early setup noise") && !strings.Contains(got, "verify_xlsx.py") {
		t.Fatalf("digest kept setup noise but lost the verifier: %q", got)
	}
}

func TestSideEffectDigest_KeepsLargeShellFailureFooter(t *testing.T) {
	a, _ := synthAgent("s")
	call := llm.ToolCall{ID: "fail", Type: "function"}
	call.Function.Name = "shell_exec"
	call.Function.Arguments = `{"command":"generate report"}`
	result := strings.Repeat("stdout noise ", 80) +
		"\nERRTAIL: permission denied\n[exit code 7]\n[aura_shell {\"exit_code\":7,\"timed_out\":false}]"
	a.history = append(a.history,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "fail", Content: result},
	)

	got := a.sideEffectDigest()
	for _, needle := range []string{"ERRTAIL: permission denied", "[exit code 7]", "[aura_shell", `"exit_code":7`} {
		if !strings.Contains(got, needle) {
			t.Fatalf("digest missing %q from large failure footer: %q", needle, got)
		}
	}
}
