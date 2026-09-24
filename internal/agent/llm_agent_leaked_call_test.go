package agent_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// leakedGLMCall is turn 44 of a production conversation (2026-09-24), shortened: the model
// began a shell_exec call in GLM's native markup, the provider never turned it into a
// structured tool_calls delta, and the loop saved the raw markup as the final answer.
const leakedGLMCall = "<tool_call>shell_exec<arg_key>command</arg_key><arg_value>B64=$(cat <<'EOF'\n" +
	"DQBDAEwASQBFAE4AVABFAAkAUgBBAEcALgAgAFMATwBDAEkAQQBMAEUACQBGAEkATABJAEEATABFAAkA"

func TestLeakedToolCall_IsRepudiatedAndNudgedNotAnswered(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", leakedGLMCall),
		agenttest.TextChunks("stop", "recovered ok"),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("a leaked tool call surfaced an error slot (must recover): %v", err)
	}
	if last := evs[len(evs)-1]; last.LLMResponse == nil || last.LLMResponse.Content != "recovered ok" {
		t.Fatalf("terminal content = %+v, want the recovered answer, never the raw markup", last.LLMResponse)
	}
	discarded := false
	for _, ev := range evs {
		discarded = discarded || ev.Actions.DiscardStreamed
	}
	if !discarded {
		t.Error("the streamed markup was never repudiated, so the client keeps showing it")
	}
	if len(fc.Requests) != 2 {
		t.Fatalf("requests = %d, want the leak followed by exactly one nudged retry", len(fc.Requests))
	}
	nudged := false
	for _, m := range fc.Requests[1].Messages {
		if strings.Contains(m.Content, "<arg_key>") {
			t.Fatalf("the leaked markup re-entered the model context as a %s message", m.Role)
		}
		nudged = nudged || (m.Role == llm.RoleUser && strings.Contains(m.Content, "tool-calling interface"))
	}
	if !nudged {
		t.Fatalf("the retry carries no leaked-call nudge: %+v", fc.Requests[1].Messages)
	}
}

func TestLeakedToolCall_TwiceFinalizes(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", leakedGLMCall),
		agenttest.TextChunks("length", "  <TOOL_CALL>\n<tool_name>shell_exec</tool_name>"),
		agenttest.TextChunks("stop", finalizeAnswer),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("a repeated leak surfaced an error slot (must finalize): %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Fatalf("terminal content = %+v, want the synthesized answer", last.LLMResponse)
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != "tool_call_leaked" {
		t.Errorf("limit_hit = %v, want tool_call_leaked", got)
	}
}

// TestLeakedToolCall_ProseAboutTheMarkupIsAnAnswer keeps the check structural: only an
// answer that OPENS with the markup is a call the provider failed to parse.
func TestLeakedToolCall_ProseAboutTheMarkupIsAnAnswer(t *testing.T) {
	recordingProvider(t)
	const prose = "GLM wraps each call in <tool_call>…</tool_call> with <arg_key> pairs."
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", prose))
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if last := evs[len(evs)-1]; last.LLMResponse == nil || last.LLMResponse.Content != prose {
		t.Fatalf("terminal content = %+v, want the prose answer untouched", last.LLMResponse)
	}
	if len(fc.Requests) != 1 {
		t.Fatalf("requests = %d, want a single call: prose is not a leaked call", len(fc.Requests))
	}
}
