package runner

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func TestAutoTitleFailureLogsExcludeDynamicValues(t *testing.T) {
	const injected = "provider failed\r\nlevel=INFO forged=true"

	tests := []struct {
		name        string
		client      *agenttest.FakeClient
		persistErr  error
		wantMessage string
	}{
		{
			name:        "generation",
			client:      agenttest.NewFakeClient(agenttest.FakeTurn{Err: errors.New(injected)}),
			wantMessage: "runner: auto-title generation failed",
		},
		{
			name:        "persistence",
			client:      agenttest.NewFakeClient(agenttest.TextChunks("stop", "A Safe Title")),
			persistErr:  errors.New(injected),
			wantMessage: "runner: auto-title persistence failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, conv, _ := newTestRunner(t, tt.client)
			convID := newConvID(t)
			mustCreate(t, r, convID)
			seedSystemTurn(t, r, convID)
			for seq, role := range []string{llm.RoleUser, llm.RoleAssistant} {
				if err := r.Conv.AppendTurn(context.Background(), conversations.AppendTurnParams{
					ConversationID: convID,
					Seq:            seq + 2,
					Role:           role,
					Content:        "threshold",
				}); err != nil {
					t.Fatalf("seed threshold turn: %v", err)
				}
			}
			conv.titleErr = tt.persistErr

			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			r.maybeAutoTitle(context.Background(), convID, []llm.Message{
				{Role: llm.RoleUser, Content: "request"},
				{Role: llm.RoleAssistant, Content: "response"},
			})
			if !r.waitWorkers(2 * time.Second) {
				t.Fatal("auto-title worker did not finish")
			}

			got := logs.String()
			if !strings.Contains(got, tt.wantMessage) {
				t.Fatalf("log = %q, want stable message %q", got, tt.wantMessage)
			}
			if strings.Contains(got, injected) || strings.Contains(got, convID) {
				t.Fatalf("log exposed a dynamic value: %q", got)
			}
		})
	}
}
