package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestSpan_TurnTreeParenting (O-08): a run that issues one tool call then a
// text_response must record an agent.turn span with at least one tool.execute
// (carrying the bounded tool class) and at least one llm.request span. The
// tool_dispatch boundary nests under agent.turn and owns tool.execute.
// This proves the trace can show per-turn and per-tool latency (the AP-20
// mitigation), not just the flat per-LLM-call span coverage that preceded it.
func TestSpan_TurnTreeParenting(t *testing.T) {
	rec := recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"hi"}`)),
		agenttest.WithUsage(agenttest.ToolCallTurn(textResponseCall("c2", "Fatto.")),
			llm.Usage{PromptTokens: 5, CompletionTokens: 2}),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	spans := rec.Ended()

	var turnSpanID oteltrace.SpanID
	var turnSeen bool
	var llmSeen, toolBoundarySeen, toolSeen bool
	var llmParent, toolBoundaryParent, toolBoundaryID, toolParent oteltrace.SpanID
	var echoNameSeen bool
	for _, s := range spans {
		switch s.Name() {
		case "agent.turn":
			turnSeen = true
			turnSpanID = s.SpanContext().SpanID()
			if !s.SpanContext().SpanID().IsValid() {
				t.Error("agent.turn span_id is the zero (invalid) value")
			}
		case "llm.request":
			llmSeen = true
			llmParent = s.Parent().SpanID()
		case "tool_dispatch":
			toolBoundarySeen = true
			toolBoundaryParent = s.Parent().SpanID()
			toolBoundaryID = s.SpanContext().SpanID()
		case "tool.execute":
			toolSeen = true
			toolParent = s.Parent().SpanID()
			for _, kv := range s.Attributes() {
				if string(kv.Key) == "tool.class" && kv.Value.AsString() == "other" {
					echoNameSeen = true
				}
			}
		}
	}

	if !turnSeen {
		t.Fatal("no agent.turn span recorded — the turn-level span is missing")
	}
	if !llmSeen {
		t.Fatal("no llm.request span recorded")
	}
	if !toolSeen {
		t.Fatal("no tool.execute span recorded — per-tool span is missing")
	}
	if !toolBoundarySeen {
		t.Fatal("no tool_dispatch boundary span recorded")
	}
	if !echoNameSeen {
		t.Error("tool.execute span is missing the bounded tool.class=other attribute")
	}
	if llmParent != turnSpanID {
		t.Errorf("llm.request parent span_id = %v, want it nested under agent.turn %v", llmParent, turnSpanID)
	}
	if toolBoundaryParent != turnSpanID {
		t.Errorf("tool_dispatch parent span_id = %v, want agent.turn %v", toolBoundaryParent, turnSpanID)
	}
	if toolParent != toolBoundaryID {
		t.Errorf("tool.execute parent span_id = %v, want tool_dispatch %v", toolParent, toolBoundaryID)
	}
}

// TestSpan_TurnEndsOnEveryPath (O-08): the agent.turn span must End on the
// terminal path too — a single content-stop turn records exactly one agent.turn
// span (Ended) and one llm.request, both well-formed. This guards the
// multiple-return-path End discipline (defer over the iter.Seq2 closure).
func TestSpan_TurnEndsOnEveryPath(t *testing.T) {
	rec := recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", "ciao"))
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	var turns int
	for _, s := range rec.Ended() {
		if s.Name() == "agent.turn" {
			turns++
			if !s.SpanContext().SpanID().IsValid() {
				t.Error("agent.turn span_id invalid")
			}
		}
	}
	if turns != 1 {
		t.Fatalf("recorded %d agent.turn spans, want exactly 1 (one Run = one turn span, ended on the terminal path)", turns)
	}
}
