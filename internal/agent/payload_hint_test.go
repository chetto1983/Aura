package agent

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/agent/governance"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

type capturePayloadSummarizer struct {
	hints []string
}

func (c *capturePayloadSummarizer) MaybeSummarize(_ context.Context, _, parentTaskHint, _ string) *governance.SummarizedPayload {
	c.hints = append(c.hints, parentTaskHint)
	return nil
}

func TestExecuteToolCallsPassesParentTaskHintToPayloadSummarizer(t *testing.T) {
	capture := &capturePayloadSummarizer{}
	runner := &stubToolRunner{names: []string{"search"}, result: "large result"}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("Genera un PDF di riepilogo delle cose che sai su di me.")

	ExecuteToolCalls(context.Background(), runner, convCtx, "user1", 0,
		[]llm.ToolCall{{ID: "call-1", Name: "search"}},
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
	reg.Register(&fakeTool{name: "search", result: "large result"})
	state := newAgentState([]llm.Message{{Role: "user", Content: "cosa e cambiato nella wiki oggi?"}})
	exec := newAgentExecutor(reg, state, nil, []string{"search"}, "", "run-hint", 0, 0, nil, false, "", nil, capture)

	exec.ExecuteToolCalls(authorizedExecCtx(), []llm.ToolCall{{ID: "call-1", Name: "search"}})

	if len(capture.hints) != 1 {
		t.Fatalf("hints = %#v, want one", capture.hints)
	}
	if capture.hints[0] != "cosa e cambiato nella wiki oggi?" {
		t.Fatalf("hint = %q", capture.hints[0])
	}
}
