package orchestration

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactTraceValueRemovesSecrets(t *testing.T) {
	input := strings.Join([]string{
		"LLM_API_KEY=sk-secret123",
		"Authorization: Bearer api-super-secret",
		"TELEGRAM_TOKEN=123456:ABCDEF",
	}, "\n")

	got := RedactTraceValue(input)
	for _, leaked := range []string{"sk-secret123", "api-super-secret", "123456:ABCDEF"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted value leaked %q in %q", leaked, got)
		}
	}
	for _, want := range []string{"LLM_API_KEY=", "Authorization: Bearer", "TELEGRAM_TOKEN="} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted value missing context %q in %q", want, got)
		}
	}
}

func TestBeforeToolCallRejectsHiddenTools(t *testing.T) {
	hooks := DefaultHooks{}
	err := hooks.BeforeToolCall(TraceEvent{
		ToolName:     "execute_code",
		ToolsExposed: []string{"search_memory", "read_wiki"},
	})
	if err == nil {
		t.Fatal("BeforeToolCall returned nil, want hidden tool rejection")
	}
	if !errors.Is(err, ErrHiddenTool) {
		t.Fatalf("BeforeToolCall error = %v, want ErrHiddenTool", err)
	}
}

func TestTraceEventSummaryRedactsMetadata(t *testing.T) {
	event := TraceEvent{
		PromptVersion: "aura-agent-v1",
		PromptHash:    "abc123",
		ToolProfile:   string(ProfileCompute),
		Metadata: map[string]string{
			"raw": "LLM_API_KEY=sk-leaky",
		},
	}

	summary := event.RedactedSummary()
	if summary["prompt_version"] != "aura-agent-v1" {
		t.Fatalf("prompt_version summary = %q", summary["prompt_version"])
	}
	if strings.Contains(summary["raw"], "sk-leaky") {
		t.Fatalf("summary leaked secret: %+v", summary)
	}
}
