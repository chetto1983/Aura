package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent/governance"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

type capturePayloadSummarizer struct {
	hints []string
	tools []string
}

func (c *capturePayloadSummarizer) MaybeSummarize(_ context.Context, toolName, parentTaskHint, _ string) *governance.SummarizedPayload {
	c.tools = append(c.tools, toolName)
	c.hints = append(c.hints, parentTaskHint)
	return nil
}

func TestExecuteToolCallsPassesParentTaskHintToPayloadSummarizer(t *testing.T) {
	capture := &capturePayloadSummarizer{}
	runner := &stubToolRunner{names: []string{"web"}, result: "large result"}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("Genera un PDF di riepilogo delle cose che sai su di me.")

	ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0,
		[]llm.ToolCall{{ID: "call-1", Name: "web", Arguments: map[string]any{"action": "fetch"}}},
		true, nil, WithPayloadSummarizer(capture))

	if len(capture.hints) != 1 {
		t.Fatalf("hints = %#v, want one", capture.hints)
	}
	if capture.hints[0] != "Genera un PDF di riepilogo delle cose che sai su di me." {
		t.Fatalf("hint = %q", capture.hints[0])
	}
}

func TestAgentExecutorPassesParentTaskHintToPayloadSummarizer(t *testing.T) {
	capture := &capturePayloadSummarizer{}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "web", result: "large result"})
	state := newAgentState([]llm.Message{{Role: "user", Content: "cosa e cambiato nella wiki oggi?"}})
	exec := newAgentExecutor(reg, state, nil, []string{"web"}, "", "run-hint", 0, 0, nil, false, "", nil, capture)

	exec.ExecuteToolCalls(authorizedExecCtx(), []llm.ToolCall{{ID: "call-1", Name: "web", Arguments: map[string]any{"action": "fetch"}}})

	if len(capture.hints) != 1 {
		t.Fatalf("hints = %#v, want one", capture.hints)
	}
	if capture.hints[0] != "cosa e cambiato nella wiki oggi?" {
		t.Fatalf("hint = %q", capture.hints[0])
	}
}

func TestExecuteToolCallsSkipsPayloadSummarizerForEvidenceClassSearch(t *testing.T) {
	capture := &capturePayloadSummarizer{}
	runner := &stubToolRunner{names: []string{"search"}, result: "CAPSULE wiki_subgraph\nSEED_CONTENT exact-slug"}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("che collegamenti ci sono nella wiki?")

	summary := ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0,
		[]llm.ToolCall{{ID: "call-1", Name: "search", Arguments: map[string]any{"action": "subgraph"}}},
		true, nil, WithPayloadSummarizer(capture))

	if len(capture.tools) != 0 {
		t.Fatalf("summarizer tools = %#v, want none for evidence-class search", capture.tools)
	}
	if summary.Results["call-1"] != "CAPSULE wiki_subgraph\nSEED_CONTENT exact-slug" {
		t.Fatalf("result = %q, want raw evidence preserved", summary.Results["call-1"])
	}
}

func TestAgentExecutorSkipsPayloadSummarizerForEvidenceClassSearch(t *testing.T) {
	capture := &capturePayloadSummarizer{}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "search", result: "CAPSULE wiki_subgraph\nSEED_SNIPPET exact-slug"})
	state := newAgentState([]llm.Message{{Role: "user", Content: "che collegamenti ci sono nella wiki?"}})
	exec := newAgentExecutor(reg, state, nil, []string{"search"}, "", "run-evidence", 0, 0, nil, false, "", nil, capture)

	summary := exec.ExecuteToolCalls(authorizedExecCtx(), []llm.ToolCall{
		{ID: "call-1", Name: "search", Arguments: map[string]any{"action": "subgraph"}},
	})

	if len(capture.tools) != 0 {
		t.Fatalf("summarizer tools = %#v, want none for evidence-class search", capture.tools)
	}
	if !strings.Contains(summary.Results["call-1"], "SEED_SNIPPET exact-slug") {
		t.Fatalf("result = %q, want raw evidence preserved", summary.Results["call-1"])
	}
}
