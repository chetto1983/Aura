package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

func TestSummarizeRefusesBeforeItReachesTheProvider(t *testing.T) {
	t.Parallel()
	rounds := []llm.Message{{Role: llm.RoleUser, Content: "hello"}}

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		var s *llmSummarizer
		if _, err := s.Summarize(context.Background(), rounds); err == nil {
			t.Fatal("a nil summarizer summarized")
		}
	})

	t.Run("empty transcript", func(t *testing.T) {
		t.Parallel()
		s := NewLLMSummarizer(&scriptedSummaryClient{}, "m", 200000, time.Second)
		if _, err := s.Summarize(context.Background(), nil); err == nil {
			t.Fatal("an empty transcript was summarized")
		}
	})

	t.Run("non-positive total timeout", func(t *testing.T) {
		t.Parallel()
		s := NewLLMSummarizer(&scriptedSummaryClient{}, "m", 200000, 0)
		_, err := s.Summarize(context.Background(), rounds)
		if err == nil || !strings.Contains(err.Error(), "total timeout") {
			t.Fatalf("a zero total timeout was accepted: %v", err)
		}
	})
}

func TestSummarizeSurfacesStreamFailures(t *testing.T) {
	t.Parallel()
	rounds := []llm.Message{{Role: llm.RoleUser, Content: "hello"}}
	openFailure := errors.New("provider refused the stream")
	midFailure := errors.New("provider dropped mid-stream")

	t.Run("stream cannot open", func(t *testing.T) {
		t.Parallel()
		s := NewLLMSummarizer(&scriptedSummaryClient{openErr: openFailure}, "m", 200000, time.Second)
		if _, err := s.Summarize(context.Background(), rounds); !errors.Is(err, openFailure) {
			t.Fatalf("open failure was swallowed: %v", err)
		}
	})

	t.Run("stream fails mid-flight", func(t *testing.T) {
		t.Parallel()
		s := NewLLMSummarizer(&scriptedSummaryClient{chunkErr: midFailure}, "m", 200000, time.Second)
		if _, err := s.Summarize(context.Background(), rounds); !errors.Is(err, midFailure) {
			t.Fatalf("mid-stream failure was swallowed: %v", err)
		}
	})
}

type scriptedSummaryClient struct {
	openErr  error
	chunkErr error
}

func (c *scriptedSummaryClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	if c.openErr != nil {
		return nil, c.openErr
	}
	ch := make(chan llm.Chunk, 1)
	if c.chunkErr != nil {
		ch <- llm.Chunk{Err: c.chunkErr}
	} else {
		ch <- llm.Chunk{Text: "summary", FinishReason: "stop"}
	}
	close(ch)
	return ch, nil
}

func TestValidateFinalRequestBudgetSkipsAnUnknownWindow(t *testing.T) {
	t.Parallel()
	// A zero window is the legacy hand-built client; boot validation rejects it in
	// production, so here it means "do not preflight" rather than "reject".
	got, err := ValidateFinalRequestBudget(llm.Request{}, llm.Config{})
	if err != nil || got != 0 {
		t.Fatalf("ValidateFinalRequestBudget(zero window) = %d, %v; want 0, nil", got, err)
	}
}

func TestValidateFinalRequestBudgetDropsToolsTheRequestForbids(t *testing.T) {
	t.Parallel()
	// ToolChoice "none" means the manifest never reaches the provider, so counting it
	// would reject requests that actually fit.
	tools := make([]llm.ToolDef, 0, 64)
	for i := range 64 {
		var tool llm.ToolDef
		tool.Type = "function"
		tool.Function.Name = "tool_" + strings.Repeat("x", 16) + string(rune('a'+i%26))
		tool.Function.Description = strings.Repeat("a long tool description ", 40)
		tools = append(tools, tool)
	}
	cfg := llm.Config{ContextWindow: 32000, MaxOutputTokens: 1000}
	req := llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, Tools: tools}

	withTools, errWith := ValidateFinalRequestBudget(req, cfg)
	req.ToolChoice = "none"
	withoutTools, errWithout := ValidateFinalRequestBudget(req, cfg)
	if errWithout != nil {
		t.Fatalf("a request that forbids tools was rejected: %v", errWithout)
	}
	if errWith == nil && withoutTools >= withTools {
		t.Fatalf("tools were still counted: with=%d without=%d", withTools, withoutTools)
	}
}

func TestRenderHistoryForTitleTruncatesAndStopsEarly(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("z", 900)
	history := make([]llm.Message, 0, 11)
	history = append(history, llm.Message{Role: llm.RoleUser, Content: long})
	history = append(history, llm.Message{Role: llm.RoleAssistant, Content: ""})
	for range 9 {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: "turn"})
	}
	got := renderHistoryForTitle(history)
	if strings.Count(got, "\n") != 5 {
		t.Fatalf("rendered %d lines, want the first 6 turns only:\n%s", strings.Count(got, "\n")+1, got)
	}
	if strings.Count(got, "z") != 500 {
		t.Fatalf("long turn was not truncated to the per-turn cap: %d chars", strings.Count(got, "z"))
	}
}

func TestReadToolOutputSpillIDFallsBackWhenTheFooterCarriesNoID(t *testing.T) {
	t.Parallel()
	if got := readToolOutputSpillID(spillFooterMarker+" 10 bytes]", "fallback"); got != "fallback" {
		t.Fatalf("readToolOutputSpillID = %q, want the fallback", got)
	}
	if got := readToolOutputSpillID("no footer at all", "fallback"); got != "fallback" {
		t.Fatalf("readToolOutputSpillID(no footer) = %q, want the fallback", got)
	}
}

func TestDropOldestRoundEmptiesABodyWithNoLaterUserTurn(t *testing.T) {
	t.Parallel()
	if _, dropped := dropOldestRound(nil); dropped {
		t.Fatal("an empty body reported a dropped round")
	}
	body := []Turn{
		{Seq: 2, Role: llm.RoleUser, Content: "ask"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "answer"},
	}
	got, dropped := dropOldestRound(body)
	if !dropped || len(got) != 0 {
		t.Fatalf("dropOldestRound = %+v, %v; want an empty body reported as dropped", got, dropped)
	}
}

func TestDropOldestPairsTrimsToolTurnsLeftLeading(t *testing.T) {
	t.Parallel()
	// Dropping a round can leave the history starting on a tool turn whose assistant
	// call is gone; a provider rejects that message, so the leftovers are trimmed.
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	bulk := strings.Repeat("padding ", 400)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{Seq: 2, Role: llm.RoleUser, Content: bulk},
		{Seq: 3, Role: llm.RoleAssistant, Content: bulk},
		{Seq: 4, Role: llm.RoleTool, ToolCallID: "t1", Content: bulk},
		{Seq: 5, Role: llm.RoleTool, ToolCallID: "t2", Content: bulk},
		{Seq: 6, Role: llm.RoleUser, Content: "active"},
	}
	reduced, pairsDropped := dropOldestPairs(enc, turns, 40)
	if pairsDropped == 0 {
		t.Fatalf("nothing was dropped from a history far over the cap: %+v", reduced)
	}
	for _, turn := range reduced {
		if turn.Role == llm.RoleTool {
			t.Fatalf("a leading orphan tool turn survived: %+v", reduced)
		}
	}
}
