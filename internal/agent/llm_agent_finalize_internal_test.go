package agent

// White-box tests for the unexported finalize helpers. They pin invariants the
// black-box (package agent_test) suite cannot reach through FakeClient — most
// importantly that synthesize() stamps ToolChoice="none" and the SessionID onto
// the wire request (a black-box client ignores both, so only a recorded-request
// assertion kills those mutants), plus the deterministic stub's tool-label
// fallback and the truncateBytes rune-boundary walk. Co-located here (not in the
// external suite) so the assertions can read req fields and call the unexported
// funcs directly. A local recordingClient is used because agenttest imports this
// package (an internal test importing it would be an import cycle).

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// recordingClient is a minimal in-package llm.Client: it captures the last request
// and replays a fixed chunk script on a pre-closed channel (goleak-clean, no
// goroutine). Local to avoid the agenttest→agent import cycle.
type recordingClient struct {
	last   llm.Request
	chunks []llm.Chunk
}

func (r *recordingClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	r.last = req
	ch := make(chan llm.Chunk, len(r.chunks)+1)
	for _, c := range r.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// synthAgent builds a minimal LlmAgent over a recordingClient for the white-box
// synthesize/stub assertions (no Run loop, no budget).
func synthAgent(sessionID string, chunks ...llm.Chunk) (*LlmAgent, *recordingClient) {
	rc := &recordingClient{chunks: chunks}
	a := NewLlmAgent(LlmAgentConfig{
		Client:    rc,
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 5},
		Registry:  tools.NewRegistry(),
		SessionID: sessionID,
	})
	return a, rc
}

// TestSynthesize_StampsToolChoiceAndSession proves the finalize synthesis request
// carries ToolChoice="none" (the tool-free invariant — Req#2/Req#1) and the
// agent's SessionID, and that the tail message is the D-03 nudge. A black-box
// client ignores both fields, so these assertions kill any mutant dropping
// `req.ToolChoice = "none"` or `req.SessionID = a.sessionID`.
func TestSynthesize_StampsToolChoiceAndSession(t *testing.T) {
	const sess = "sess-finalize-xyz"
	a, rc := synthAgent(sess, llm.Chunk{Text: "sintesi"})

	answer, _, err := a.synthesize(InvocationContext{Ctx: context.Background()})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if answer != "sintesi" {
		t.Errorf("answer = %q, want the streamed content %q", answer, "sintesi")
	}
	if rc.last.ToolChoice != "none" {
		t.Errorf("finalize request ToolChoice = %q, want \"none\" (tool-free synthesis invariant)", rc.last.ToolChoice)
	}
	if rc.last.SessionID != sess {
		t.Errorf("finalize request SessionID = %q, want %q", rc.last.SessionID, sess)
	}
	last := rc.last.Messages[len(rc.last.Messages)-1]
	if last.Role != llm.RoleUser || last.Content != finalizeNudge {
		t.Errorf("finalize request tail = {%v, %q}, want the user-role finalizeNudge", last.Role, last.Content)
	}
}

// TestMaybeRecover_ToolNudgeNamesTool proves the FIRST dedup trip injects a
// recovery nudge naming the offending tool (the tool-variant concatenation), and
// that a SECOND call is a no-op (counter-gated, not a re-injecting latch).
func TestMaybeRecover_ToolNudgeNamesTool(t *testing.T) {
	a, _ := synthAgent("s")
	before := len(a.history)

	if !a.maybeRecover("web_fetch") {
		t.Fatal("first maybeRecover returned false; want true (recover once)")
	}
	if len(a.history) != before+1 {
		t.Fatalf("history grew by %d, want exactly 1 nudge", len(a.history)-before)
	}
	nudge := a.history[len(a.history)-1]
	if nudge.Role != llm.RoleUser || !strings.Contains(nudge.Content, "web_fetch") {
		t.Errorf("recovery nudge = {%v, %q}, want a user-role nudge naming web_fetch", nudge.Role, nudge.Content)
	}
	if !strings.HasPrefix(nudge.Content, recoveryNudgeToolPrefix) {
		t.Errorf("tool-variant nudge missing its prefix; got %q", nudge.Content)
	}

	if a.maybeRecover("web_fetch") {
		t.Error("second maybeRecover returned true; the counter must gate to false after one recovery")
	}
	if len(a.history) != before+1 {
		t.Errorf("second maybeRecover injected another nudge (history=%d); want no-op", len(a.history))
	}
}

// TestStubDigest_UnknownCallIDLabelsTool proves the stub labels a RoleTool whose
// ToolCallID has no matching assistant tool-call with the generic "tool" name
// (the fallback branch), and always leads with stubLeadIn.
func TestStubDigest_UnknownCallIDLabelsTool(t *testing.T) {
	a, _ := synthAgent("s")
	a.history = append(a.history,
		llm.Message{Role: llm.RoleTool, ToolCallID: "orphan", Content: "dato raccolto"},
	)
	got := a.stubDigest()
	if !strings.HasPrefix(got, stubLeadIn) {
		t.Fatalf("stub digest = %q, want the stubLeadIn prefix", got)
	}
	if !strings.Contains(got, "- tool: dato raccolto") {
		t.Errorf("unknown-callID bullet = %q, want the generic \"- tool: \" label", got)
	}
}

// TestTruncateBytes covers the byte clamp on a valid UTF-8 boundary: the len<=n
// early return (exact-length boundary, where an off-by-one would index past the
// end), ASCII truncation, and the rune-boundary walk that must never split a
// multibyte rune (and must terminate — a mutant that stops decrementing hangs).
func TestTruncateBytes(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter_than_n", "abc", 5, "abc"},
		{"exact_length", "abc", 3, "abc"}, // len(s)==n returns s, must not walk past the end
		{"ascii_truncate", "hello", 3, "hel"},
		{"multibyte_boundary", "àbc", 1, ""}, // byte 1 splits 'à' (0xC3 0xA0) → walk back to 0
		{"multibyte_keeps_whole_rune", "àbc", 2, "à"},
		{"empty", "", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateBytes(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("truncateBytes(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}
}
