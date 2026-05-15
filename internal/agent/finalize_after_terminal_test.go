package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

type stubTerminalFinalizer struct {
	model  string
	chatFn func(ctx context.Context, req llm.Request) (llm.Response, error)
}

func (s *stubTerminalFinalizer) FinalizeModel() string { return s.model }

func (s *stubTerminalFinalizer) FinalizeChat(ctx context.Context, req llm.Request) (llm.Response, error) {
	if s.chatFn != nil {
		return s.chatFn(ctx, req)
	}
	return llm.Response{}, nil
}

func (s *stubTerminalFinalizer) FinalizeRecordUsage(llm.TokenUsage) {}
func (s *stubTerminalFinalizer) FinalizeEstimateCost(llm.TokenUsage) float64 { return 0 }

func TestFinalizeAfterTerminalToolSuccess(t *testing.T) {
	runner := &stubTerminalFinalizer{
		chatFn: func(_ context.Context, _ llm.Request) (llm.Response, error) {
			return llm.Response{Content: "Il file è stato creato con successo."}, nil
		},
	}
	convCtx := conversation.NewContext(conversation.Config{})
	stats := &TurnStats{TerminalTool: "write_file"}

	text, ok := FinalizeAfterTerminalTool(context.Background(), runner, convCtx, `{"ok":true}`, stats)

	if !ok {
		t.Fatalf("FinalizeAfterTerminalTool ok = false, want true on success")
	}
	if text != "Il file è stato creato con successo." {
		t.Fatalf("text = %q, want prose response", text)
	}
	if stats.LLMCalls != 1 || stats.LoopSteps != 1 {
		t.Fatalf("stats.LLMCalls=%d LoopSteps=%d, want 1/1", stats.LLMCalls, stats.LoopSteps)
	}
	msgs := convCtx.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" {
		t.Fatalf("convCtx messages = %+v, want 1 assistant message", msgs)
	}
}

func TestFinalizeAfterTerminalToolEmptyFallback(t *testing.T) {
	// Simulate LLM returning tool-call markup on both attempts → Fallback=true, Text="".
	runner := &stubTerminalFinalizer{
		chatFn: func(_ context.Context, _ llm.Request) (llm.Response, error) {
			return llm.Response{Content: `<tool_call name="x">`}, nil
		},
	}
	convCtx := conversation.NewContext(conversation.Config{})
	stats := &TurnStats{TerminalTool: "write_file"}

	text, ok := FinalizeAfterTerminalTool(context.Background(), runner, convCtx, "", stats)

	if ok {
		t.Fatalf("FinalizeAfterTerminalTool ok = true, want false for fallback (empty) text")
	}
	if text != "" {
		t.Fatalf("text = %q, want empty string on fallback", text)
	}
}

func TestFinalizeAfterTerminalToolLLMError(t *testing.T) {
	runner := &stubTerminalFinalizer{
		chatFn: func(_ context.Context, _ llm.Request) (llm.Response, error) {
			return llm.Response{}, errors.New("connection refused")
		},
	}
	convCtx := conversation.NewContext(conversation.Config{})
	stats := &TurnStats{TerminalTool: "execute_code"}

	_, ok := FinalizeAfterTerminalTool(context.Background(), runner, convCtx, "err output", stats)

	if ok {
		t.Fatalf("FinalizeAfterTerminalTool ok = true, want false on LLM error")
	}
	if stats.LLMCalls != 1 {
		t.Fatalf("stats.LLMCalls = %d, want 1", stats.LLMCalls)
	}
}
