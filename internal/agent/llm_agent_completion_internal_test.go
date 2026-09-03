package agent

// White-box tests for the completion-gate helpers that the black-box suite cannot
// reach: the verdict parser (NOT_DONE must win over the DONE substring), the
// reason extractor, the nudge-skipping request finder, and the side-effect digest.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// TestRunCompletionCritic_SeversAlreadyExpiredIcCtxDeadline is the fix-plan 1.1
// RED test for the completion critic (mirrors TestSynthesize_SeversAlreadyExpiredIcCtxDeadline
// in llm_agent_finalize_internal_test.go): the critic is a salvage-adjacent
// tool-free call that shares the same context.WithTimeout(ic.Ctx, ...) derivation.
// When ic.Ctx already carries an expired deadline (the wallclock-trip production
// shape, internal/runner/runner.go's budget.WithDeadline), the critic call must NOT
// inherit that Done-ness — it needs a fresh deadline to actually run.
func TestRunCompletionCritic_SeversAlreadyExpiredIcCtxDeadline(t *testing.T) {
	a, rc := synthAgent("sess-critic-expired", llm.Chunk{Text: "DONE"}, llm.Chunk{FinishReason: "stop"})

	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	if expiredCtx.Err() == nil {
		t.Fatal("test setup: expiredCtx must already be Done")
	}

	done, _, ok := a.runCompletionCritic(InvocationContext{Ctx: expiredCtx}, "the answer")
	if !ok {
		t.Fatal("runCompletionCritic ok = false, want true (the salvage call must actually run, not fail open on a DOA ctx)")
	}
	if !done {
		t.Error("runCompletionCritic done = false, want true (verdict DONE)")
	}
	if rc.ctxErr != nil {
		t.Fatalf("critic call ctx.Err() = %v, want nil (a fresh deadline, not inherited from the expired ic.Ctx)", rc.ctxErr)
	}
	if remaining := time.Until(rc.deadline); remaining < 4*time.Second {
		t.Fatalf("critic call deadline remaining = %s, want about 5s (fresh TotalTimeoutSec)", remaining)
	}
}

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
	for i := range 20 {
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

// TestTruncateTailBytes mirrors TestTruncateBytes (the head clamp) for the TAIL
// keep used by sideEffectDigest: the n<=0 and len(s)<=n early returns, an ASCII
// tail clamp, the multibyte mid-rune walk-back (the byte budget splits a leading
// rune so start must advance to the next rune start), and the case where the tail
// boundary already sits on a rune start. Every output must be valid UTF-8 — a split
// rune would corrupt the critic prompt the digest feeds.
func TestTruncateTailBytes(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"n_zero", "abc", 0, ""},
		{"n_negative", "abc", -1, ""},
		{"shorter_than_n", "abc", 5, "abc"},
		{"exact_length", "abc", 3, "abc"}, // len(s)==n returns s, must not index past the end
		{"ascii_tail", "hello", 3, "llo"},
		{"multibyte_mid_rune_walkback", "àbc", 3, "bc"}, // start lands on à's 0xA0 cont. byte → advance to 'b'
		{"multibyte_keeps_whole_rune", "abà", 2, "à"},   // tail boundary on à's rune start → whole rune kept
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTailBytes(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("truncateTailBytes(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateTailBytes(%q, %d) = %q is not valid UTF-8 (split rune)", tc.s, tc.n, got)
			}
		})
	}
}

// TestTruncateBytesKeepingTail covers the composing helper: the n<=0 and len(s)<=n
// early returns, the n<=len(marker) passthrough to truncateTailBytes (the marker
// would not fit, so only the tail is kept), and the head+marker+tail composition
// for both an ASCII and a multibyte string. The multibyte case proves the marker is
// flanked by rune-clean head/tail clamps so the whole digest stays valid UTF-8.
func TestTruncateBytesKeepingTail(t *testing.T) {
	const marker = "\n...[truncated]...\n" // local copy of the function's private const (19 bytes)

	asciiHT := strings.Repeat("a", 30) + strings.Repeat("b", 30) // 60 bytes
	multiHT := strings.Repeat("à", 20)                           // 40 bytes (2 bytes/rune)

	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"n_zero", "abcdef", 0, ""},
		{"len_le_n", "abc", 10, "abc"},
		{"n_le_marker_passthrough", "0123456789ABCDEF", 10, "6789ABCDEF"}, // 10<=19 → tail-only
		{"head_marker_tail_ascii", asciiHT, 39, strings.Repeat("a", 10) + marker + strings.Repeat("b", 10)},
		{"head_marker_tail_multibyte", multiHT, 25, "à" + marker + "à"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateBytesKeepingTail(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("truncateBytesKeepingTail(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateBytesKeepingTail(%q, %d) = %q is not valid UTF-8 (split rune)", tc.s, tc.n, got)
			}
		})
	}
}

// TestSideEffectDigest_ExcludesPriorTurnToolResults pins the digest to THIS run.
//
// Live incident 2026-09-03 05:57 (thread 01a063fb): asked "adesso che ora è?" the
// agent answered 07:57 correctly and with no tool call at all, yet the critic
// vetoed twice — "The proposed answer (07:57) contradicts the tool result
// (21:17)" — because the digest walked the whole rehydrated history and handed it
// a current_time result from the PREVIOUS evening as this turn's evidence. Three
// loop calls plus two critic calls to re-derive the same 14 characters.
//
// The contract was never in doubt: the critic's own system prompt promises "the
// tool calls the agent made this turn", so a stale result is not merely noise, it
// is evidence the critic is told to trust.
func TestSideEffectDigest_ExcludesPriorTurnToolResults(t *testing.T) {
	stale := llm.ToolCall{ID: "yesterday", Type: "function"}
	stale.Function.Name = "current_time"
	stale.Function.Arguments = `{}`

	a := NewLlmAgent(LlmAgentConfig{
		Client:    &recordingClient{},
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 5},
		Registry:  tools.NewRegistry(),
		SessionID: "s",
		UserTurns: []llm.Message{
			{Role: llm.RoleUser, Content: "che ora è?"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{stale}},
			{Role: llm.RoleTool, ToolCallID: "yesterday", Content: "Wednesday, 2026-09-02T21:17:01Z"},
			{Role: llm.RoleAssistant, Content: "Sono le 23:17."},
			{Role: llm.RoleUser, Content: "adesso che ora è?"},
		},
	})

	got := a.sideEffectDigest()
	if strings.Contains(got, "21:17") || strings.Contains(got, "current_time") {
		t.Fatalf("digest leaked a prior turn's tool result into this turn's evidence: %q", got)
	}
	if got != "\n(no tool calls)" {
		t.Fatalf("digest = %q, want the empty-evidence marker for a turn that dispatched nothing", got)
	}
}

// TestSideEffectDigest_KeepsThisRunAfterRehydration is the other half: scoping the
// digest must not blind the critic to the run it is actually judging.
func TestSideEffectDigest_KeepsThisRunAfterRehydration(t *testing.T) {
	stale := llm.ToolCall{ID: "yesterday", Type: "function"}
	stale.Function.Name = "current_time"
	stale.Function.Arguments = `{}`

	a := NewLlmAgent(LlmAgentConfig{
		Client:    &recordingClient{},
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 5},
		Registry:  tools.NewRegistry(),
		SessionID: "s",
		UserTurns: []llm.Message{
			{Role: llm.RoleUser, Content: "che ora è?"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{stale}},
			{Role: llm.RoleTool, ToolCallID: "yesterday", Content: "Wednesday, 2026-09-02T21:17:01Z"},
			{Role: llm.RoleAssistant, Content: "Sono le 23:17."},
			{Role: llm.RoleUser, Content: "scrivi il report"},
		},
	})
	fresh := llm.ToolCall{ID: "now", Type: "function"}
	fresh.Function.Name = "fs_write"
	fresh.Function.Arguments = `{"path":"/tmp/report.md"}`
	a.history = append(a.history,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{fresh}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "now", Content: "wrote 42 bytes to /tmp/report.md"},
	)

	got := a.sideEffectDigest()
	if !strings.Contains(got, "fs_write(") || !strings.Contains(got, "wrote 42 bytes") {
		t.Fatalf("digest lost this run's own evidence: %q", got)
	}
	if strings.Contains(got, "21:17") {
		t.Fatalf("digest still leaked the prior turn: %q", got)
	}
}

// TestCompletionCriticUser_CarriesGrounding pins the fix for the 2026-09-03 07:14
// veto: the critic must receive the same volatile facts the turn's own prompt
// carried, or an answer drawn from them is unverifiable to it by construction.
//
// The live verdict was "The current time was not verified via a tool call." on an
// answer the agent had read straight out of <current_time>. Six LLM calls followed.
func TestCompletionCriticUser_CarriesGrounding(t *testing.T) {
	a, _ := synthAgent("s")
	a.workspace = "/workspace/alice"
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := InvocationContext{Ctx: context.Background(), Budget: budget}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: "che ora è?"})

	got := a.completionCriticUser(ic, "Sono le 09:14.")

	for _, needle := range []string{
		"<context_given_to_agent>", "<current_time>", "<today>", "<workspace>/workspace/alice</workspace>",
		"<user_request>", "<tool_activity>", "<proposed_final_answer>",
	} {
		if !strings.Contains(got, needle) {
			t.Errorf("critic context missing %q:\n%s", needle, got)
		}
	}
	// Instructions to the model are not evidence about the world: a step budget or a
	// roster of unloaded tools tells the critic nothing about whether the answer holds.
	for _, forbidden := range []string{"<budget>", "<deferred_tools>"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("critic context leaked model instructions %q:\n%s", forbidden, got)
		}
	}
}

// TestCompletionCriticUser_OmitsGroundingWhenThereIsNone keeps the empty case silent.
// An empty <context_given_to_agent> would invite the critic to read the absence of
// facts as evidence against the answer — the exact inference this section exists to
// stop.
func TestCompletionCriticUser_OmitsGroundingWhenThereIsNone(t *testing.T) {
	a, _ := synthAgent("s")
	got := a.completionCriticUser(InvocationContext{Ctx: context.Background()}, "fatto")
	if strings.Contains(got, "context_given_to_agent") {
		t.Fatalf("a Budget-less turn must ground on nothing, not on an empty block:\n%s", got)
	}
	if !strings.Contains(got, "<proposed_final_answer>") {
		t.Fatalf("critic context lost its answer section:\n%s", got)
	}
}
