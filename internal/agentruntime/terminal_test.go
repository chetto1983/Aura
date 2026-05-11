package agentruntime

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestFinalizeTerminalToolUsesNoToolLLMAndTracksUsage(t *testing.T) {
	var gotRequest llm.Request
	result := FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		Messages:      []llm.Message{{Role: "tool", Content: "created file"}},
		TerminalTool:  "write_file",
		RawToolResult: `{"ok":true}`,
		Model:         "test-model",
		Send: func(_ context.Context, req llm.Request) (llm.Response, error) {
			gotRequest = req
			return llm.Response{
				Content: "Ho aggiornato il file.",
				Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			}, nil
		},
		EstimateCost: func(usage llm.TokenUsage) float64 {
			return float64(usage.TotalTokens) / 1000
		},
		RecordUsage: func(usage llm.TokenUsage) {
			if usage.TotalTokens != 15 {
				t.Fatalf("RecordUsage total = %d, want 15", usage.TotalTokens)
			}
		},
	})

	if result.Text != "Ho aggiornato il file." || result.Fallback {
		t.Fatalf("FinalizeTerminalTool() = %+v, want LLM text without fallback", result)
	}
	if gotRequest.Model != "test-model" || len(gotRequest.Tools) != 0 {
		t.Fatalf("request = %+v, want model with no tools", gotRequest)
	}
	if result.TokensTotal != 15 || result.CostUSD != 0.015 {
		t.Fatalf("usage = %+v, want tracked usage and cost", result)
	}
}

// TestFinalizeTerminalToolRetriesOnFailedSynthesis covers the new contract:
// when the LLM returns tool-call markup or empty text, we retry once with a
// stricter prompt before giving up. The second attempt's plain prose is the
// answer the user sees — never a hardcoded canned string.
func TestFinalizeTerminalToolRetriesOnFailedSynthesis(t *testing.T) {
	var attempts atomic.Int32
	result := FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		TerminalTool:  "execute_shell",
		RawToolResult: "exit_code: 0\n\nconnection refused",
		Send: func(_ context.Context, _ llm.Request) (llm.Response, error) {
			n := attempts.Add(1)
			if n == 1 {
				// First attempt emits tool-call markup → must trigger retry.
				return llm.Response{Content: `<tool_call name="anything">`}, nil
			}
			return llm.Response{Content: "Ho provato curl ma il servizio ha rifiutato la connessione."}, nil
		},
	})
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want exactly 2 (initial + retry)", attempts.Load())
	}
	if result.Fallback {
		t.Fatalf("Fallback = true on successful retry, want false")
	}
	if !strings.Contains(result.Text, "rifiutato") {
		t.Fatalf("Text = %q, want the second LLM attempt's prose", result.Text)
	}
}

// TestFinalizeTerminalToolReturnsEmptyOnRepeatedFailure asserts the deliberate
// "no canned string" contract: if both LLM attempts fail, Text is empty and
// Fallback=true so the caller (Telegram) can simply send nothing rather than
// a robotic placeholder sentence.
func TestFinalizeTerminalToolReturnsEmptyOnRepeatedFailure(t *testing.T) {
	result := FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		TerminalTool: "write_file",
		Send: func(_ context.Context, _ llm.Request) (llm.Response, error) {
			return llm.Response{Content: `<tool_call name="x">`}, nil
		},
	})
	if !result.Fallback {
		t.Fatalf("Fallback = false, want true after exhausting retries")
	}
	if strings.TrimSpace(result.Text) != "" {
		t.Fatalf("Text = %q, want empty so Telegram sends nothing", result.Text)
	}
}

// TestFinalizeTerminalToolReturnsEmptyWhenSendIsNil documents the no-LLM path:
// callers that did not wire a Send (debug harness, dry-run) get Fallback=true
// and empty Text — no canned fill-in.
func TestFinalizeTerminalToolReturnsEmptyWhenSendIsNil(t *testing.T) {
	result := FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		TerminalTool: "write_file",
	})
	if !result.Fallback || result.Text != "" {
		t.Fatalf("FinalizeTerminalTool(send=nil) = %+v, want empty fallback", result)
	}
}

func TestFinalizeTerminalToolStrictPromptOnRetry(t *testing.T) {
	var firstPrompt, secondPrompt string
	_ = FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		TerminalTool: "execute_code",
		Send: func(_ context.Context, req llm.Request) (llm.Response, error) {
			last := req.Messages[len(req.Messages)-1].Content
			if firstPrompt == "" {
				firstPrompt = last
				return llm.Response{}, errors.New("transient")
			}
			secondPrompt = last
			return llm.Response{Content: "ok"}, nil
		},
	})
	if firstPrompt == "" || secondPrompt == "" {
		t.Fatalf("both attempts must record a prompt: first=%q second=%q", firstPrompt, secondPrompt)
	}
	if firstPrompt == secondPrompt {
		t.Fatal("retry must use a different (stricter) prompt")
	}
	if !strings.Contains(secondPrompt, "RETRY") {
		t.Fatalf("retry prompt missing RETRY marker: %q", secondPrompt)
	}
}

func TestLooksLikeUnsafeFinalAnswer(t *testing.T) {
	for _, raw := range []string{
		`Evidence envelope: {"items":[]}`,
		"exit_code: 0\nelapsed_ms: 3",
		`{"source_id":"src_1","tokens_total":123}`,
		"workspace_root=/workspace",
	} {
		if !LooksLikeUnsafeFinalAnswer(raw) {
			t.Fatalf("LooksLikeUnsafeFinalAnswer(%q) = false, want true", raw)
		}
	}
	if LooksLikeUnsafeFinalAnswer("Ho trovato il documento e te lo riassumo.") {
		t.Fatal("LooksLikeUnsafeFinalAnswer(natural text) = true, want false")
	}
}

func TestTerminalToolFinalizationMessagesAppendsPrompt(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "file-1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "file-1", Content: `{"ok":true}`},
	}

	got := TerminalToolFinalizationMessages(messages, "write_file")
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	last := got[len(got)-1].Content
	for _, want := range []string{"Do not call tools", "natural prose"} {
		if !strings.Contains(last, want) {
			t.Fatalf("terminal finalization instruction = %q, missing %q", last, want)
		}
	}
}

func TestTerminalToolFinalizationMessagesForSearchMemoryInstructsCitation(t *testing.T) {
	got := TerminalToolFinalizationMessages(nil, "search_memory")
	if len(got) != 1 {
		t.Fatalf("messages len = %d, want 1", len(got))
	}
	last := got[0].Content
	for _, want := range []string{"Do not call tools", "Answer the user's original request", "[[slug]]"} {
		if !strings.Contains(last, want) {
			t.Fatalf("search_memory finalization instruction = %q, want %q", last, want)
		}
	}
}

func TestLooksLikeToolCallMarkupRecognizesDSML(t *testing.T) {
	raw := `<｜｜DSML｜｜tool_calls><｜｜DSML｜｜invoke name="write_file">`
	if !LooksLikeToolCallMarkup(raw) {
		t.Fatal("LooksLikeToolCallMarkup = false, want true")
	}
}
