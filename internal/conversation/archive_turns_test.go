package conversation

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestArchiveConversationTurnsAppendsUserAndLoopMessages(t *testing.T) {
	appender := &recordingTurnArchiver{}
	logger := slog.New(slog.DiscardHandler)

	ArchiveConversationTurns(context.Background(), logger, appender, ArchiveTurnInput{
		ChatID:    42,
		UserID:    7,
		NextIndex: 10,
		UserText:  "please compute",
		LoopMessages: []llm.Message{
			{
				Role:    "assistant",
				Content: "calling",
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "execute_code",
					Arguments: map[string]any{"code": "print(5050)"},
				}},
			},
			{Role: "tool", ToolCallID: "call-1", Content: "5050"},
			{Role: "assistant", Content: "done"},
		},
		LLMCalls:  2,
		ToolCalls: 1,
		ElapsedMS: 123,
		TokensIn:  456,
	})

	if len(appender.turns) != 4 {
		t.Fatalf("appended %d turns, want 4: %+v", len(appender.turns), appender.turns)
	}

	assertArchivedConvTurn(t, appender.turns[0], Turn{
		ChatID:    42,
		UserID:    7,
		TurnIndex: 10,
		Role:      "user",
		Content:   "please compute",
	})
	if appender.turns[1].TurnIndex != 11 || appender.turns[1].Role != "assistant" || appender.turns[1].ToolCalls == "" {
		t.Fatalf("assistant tool-call turn = %+v, want index 11 with tool_calls JSON", appender.turns[1])
	}
	assertArchivedConvTurn(t, appender.turns[2], Turn{
		ChatID:     42,
		UserID:     7,
		TurnIndex:  12,
		Role:       "tool",
		Content:    "5050",
		ToolCallID: "call-1",
	})
	final := appender.turns[3]
	assertArchivedConvTurn(t, final, Turn{
		ChatID:    42,
		UserID:    7,
		TurnIndex: 13,
		Role:      "assistant",
		Content:   "done",
	})
	if final.LLMCalls != 2 || final.ToolCallsCount != 1 || final.ElapsedMS != 123 || final.TokensIn != 456 {
		t.Fatalf("final telemetry = llm=%d tools=%d elapsed=%d tokens=%d, want 2/1/123/456",
			final.LLMCalls, final.ToolCallsCount, final.ElapsedMS, final.TokensIn)
	}
}

func TestArchiveConversationTurnsLogsAppendFailures(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	appender := &failingTurnArchiver{err: errors.New("disk full")}

	ArchiveConversationTurns(context.Background(), logger, appender, ArchiveTurnInput{
		ChatID:    42,
		UserID:    7,
		NextIndex: 10,
		UserText:  "please remember this",
	})

	got := logs.String()
	for _, want := range []string{
		"archive: append failed",
		"chat_id=42",
		"turn_index=10",
		"role=user",
		`error="disk full"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}

func assertArchivedConvTurn(t *testing.T, got, want Turn) {
	t.Helper()
	if got.ChatID != want.ChatID || got.UserID != want.UserID || got.TurnIndex != want.TurnIndex ||
		got.Role != want.Role || got.Content != want.Content || got.ToolCallID != want.ToolCallID {
		t.Fatalf("turn = %+v, want core fields %+v", got, want)
	}
}

type recordingTurnArchiver struct {
	turns []Turn
}

func (r *recordingTurnArchiver) Append(_ context.Context, turn Turn) error {
	r.turns = append(r.turns, turn)
	return nil
}

type failingTurnArchiver struct {
	err error
}

func (f *failingTurnArchiver) Append(context.Context, Turn) error {
	return f.err
}
