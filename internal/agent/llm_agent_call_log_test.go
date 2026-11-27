package agent

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// slowFirstChunkClient holds its first chunk back, as a provider does while it reads the prompt.
type slowFirstChunkClient struct {
	wait   time.Duration
	chunks []llm.Chunk
}

func (c slowFirstChunkClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	go func() {
		defer close(ch)
		select {
		case <-time.After(c.wait):
		case <-ctx.Done():
			return
		}
		for _, chunk := range c.chunks {
			ch <- chunk
		}
	}()
	return ch, nil
}

// Measured on the lab VM 2026-11-27: a slow turn could not be split from the logs, because the
// call line named neither its time nor how much of the prompt the provider had cached.
func TestLLMCallEndLogsItsTimeAndTheCache(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	const wait = 30 * time.Millisecond
	a := NewLlmAgent(LlmAgentConfig{
		Client: slowFirstChunkClient{wait: wait, chunks: []llm.Chunk{
			{Text: "ciao"},
			{FinishReason: "stop"},
			{Usage: &llm.Usage{PromptTokens: 92578, CompletionTokens: 48, CachedTokens: 2048, ResponseID: "gen-1", Provider: "Inkling"}},
		}},
		LLM:        llm.Config{Model: "inkling", Provider: "openrouter", TotalTimeoutSec: 30},
		Registry:   tools.NewRegistry(),
		PreviewCap: 1024,
		RunDir:     t.TempDir(),
		SessionID:  "call-log",
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "che ore sono?"}},
	})
	b, err := NewBudget(BudgetOptions{MaxSteps: new(3)})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if _, err := drainRun(a.Run(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b})); err != nil {
		t.Fatalf("run: %v", err)
	}

	line := callEndLine(t, logs.String())
	for _, want := range []string{"model=inkling", "prompt_tokens=92578", "cached_tokens=2048", "completion_tokens=48", "response_id=gen-1", "provider=Inkling"} {
		if !strings.Contains(line, want) {
			t.Fatalf("call line lacks %q:\n%s", want, line)
		}
	}
	first, total := logMillis(t, line, "first_token_ms"), logMillis(t, line, "duration_ms")
	if first < wait.Milliseconds() || total < first {
		t.Fatalf("first_token_ms=%d duration_ms=%d, want first >= %d and total >= first", first, total, wait.Milliseconds())
	}
}

func callEndLine(t *testing.T, logs string) string {
	t.Helper()
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, `msg="agent llm call end"`) {
			return line
		}
	}
	t.Fatalf("no call end line in:\n%s", logs)
	return ""
}

func logMillis(t *testing.T, line, key string) int64 {
	t.Helper()
	_, rest, ok := strings.Cut(line, " "+key+"=")
	if !ok {
		t.Fatalf("call line lacks %s:\n%s", key, line)
	}
	value, _, _ := strings.Cut(rest, " ")
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatalf("%s=%q is not a number", key, value)
	}
	return n
}
