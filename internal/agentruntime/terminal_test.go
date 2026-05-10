package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestFormatTerminalExecuteCodeResultMasksMetadata(t *testing.T) {
	raw := "exit_code: 0\nelapsed_ms: 42\n\nwrote files\n\nartifacts:\n- aura_sales_summary.csv (36 bytes, text/csv, delivered=true, persisted=true, source_id=src_123)\n- aura_sales_plot.png (2048 bytes, image/png, delivered=true, persisted=true, source_id=src_456)"

	got := FormatTerminalExecuteCodeResult(raw)

	for _, leaked := range []string{"source_id", "delivered=true", "persisted=true", "text/csv", "image/png", "exit_code", "elapsed_ms"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("FormatTerminalExecuteCodeResult leaked %q in %q", leaked, got)
		}
	}
	for _, want := range []string{"wrote files", "aura_sales_summary.csv", "aura_sales_plot.png"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatTerminalExecuteCodeResult = %q, want %q", got, want)
		}
	}
}

func TestFormatTerminalExecuteCodeResultDoesNotLeakBareStderr(t *testing.T) {
	raw := "exit_code: 0\nelapsed_ms: 17\n\n--- stderr ---\nfind: '/home/user': No such file or directory"

	got := FormatTerminalExecuteCodeResult(raw)

	for _, leaked := range []string{"find:", "/home/user", "--- stderr ---", "exit_code"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("FormatTerminalExecuteCodeResult leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "non ha prodotto un risultato utile") {
		t.Fatalf("FormatTerminalExecuteCodeResult = %q, want natural failure", got)
	}
}

func TestFormatTerminalExecuteCodeResultDoesNotLeakToolErrorJSON(t *testing.T) {
	raw := `{"ok":false,"error":"shell command failed (exit=1): find: '/home/user': No such file or directory","retryable":true}`

	got := FormatTerminalExecuteCodeResult(raw)

	for _, leaked := range []string{`"ok":false`, "find:", "/home/user", "retryable"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("FormatTerminalExecuteCodeResult leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "non e riuscito") {
		t.Fatalf("FormatTerminalExecuteCodeResult = %q, want natural failure", got)
	}
}

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

func TestFinalizeTerminalToolFallsBackWhenLLMEmitsToolMarkup(t *testing.T) {
	result := FinalizeTerminalTool(context.Background(), TerminalFinalizationInput{
		TerminalTool:  "write_file",
		RawToolResult: `<｜｜DSML｜｜tool_calls>`,
		Send: func(context.Context, llm.Request) (llm.Response, error) {
			return llm.Response{Content: `<tool_call name="write_file">`}, nil
		},
	})

	if !result.Fallback {
		t.Fatalf("Fallback = false, want true")
	}
	if strings.Contains(result.Text, "tool_call") || strings.Contains(result.Text, "DSML") {
		t.Fatalf("fallback leaked tool markup: %q", result.Text)
	}
}

func TestFormatTerminalFileResultMasksRawJSON(t *testing.T) {
	raw := `{"source_id":"src_secret","filename":"report.docx","size_bytes":1234,"delivered":true}`

	got := FormatTerminalFileResult("create_docx", raw)

	for _, leaked := range []string{"src_secret", "source_id", "delivered"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("FormatTerminalFileResult leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "report.docx") || !strings.Contains(got, "inviato") {
		t.Fatalf("FormatTerminalFileResult = %q, want filename and delivery summary", got)
	}
}

func TestTerminalToolFallbackMasksInternalResultMetadata(t *testing.T) {
	raw := `{"source_id":"src_secret","metrics":{"tokens_total":123},"exit_code":0}`

	got := TerminalToolFallbackResponse("execute_code", raw)

	for _, leaked := range []string{"src_secret", "source_id", "tokens_total", "exit_code"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("TerminalToolFallbackResponse leaked %q in %q", leaked, got)
		}
	}
}

func TestTerminalToolFallbackMasksShellDump(t *testing.T) {
	raw := "Filesystem      Size  Used Avail Use% Mounted on\noverlay          100G   10G   90G  10% /\n/dev/sda /var/lib/docker"

	got := TerminalToolFallbackResponse("execute_shell", raw)

	for _, leaked := range []string{"Filesystem", "overlay", "/var/lib"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("TerminalToolFallbackResponse leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "output tecnico grezzo") {
		t.Fatalf("TerminalToolFallbackResponse = %q, want natural shell fallback", got)
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

func TestTerminalToolFinalizationMessagesBlockToolMarkup(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "file-1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "file-1", Content: `{"ok":true}`},
	}

	got := TerminalToolFinalizationMessages(messages, "write_file")
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	last := got[len(got)-1].Content
	if !strings.Contains(last, "Do not call tools") || !strings.Contains(last, "Do not emit JSON, XML, DSML, or tool-call markup") {
		t.Fatalf("terminal finalization instruction = %q", last)
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

func TestTerminalToolFallbackRejectsToolMarkup(t *testing.T) {
	raw := `<｜｜DSML｜｜tool_calls><｜｜DSML｜｜invoke name="write_file">`
	if !LooksLikeToolCallMarkup(raw) {
		t.Fatal("LooksLikeToolCallMarkup = false, want true")
	}
	got := TerminalToolFallbackResponse("write_file", raw)
	if strings.Contains(got, "DSML") || strings.Contains(got, "tool_calls") {
		t.Fatalf("fallback leaked tool markup: %q", got)
	}
	if !strings.Contains(got, "file") {
		t.Fatalf("fallback = %q, want file completion", got)
	}
}
