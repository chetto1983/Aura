package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestRunTurnCleanupOutlivesACancelledTurn(t *testing.T) {
	turnCtx, cancel := context.WithCancel(context.Background())
	ctx, cleanup := tools.WithTurnCleanup(turnCtx)
	stepErr := errors.New("step never ran")
	cleanup.Add("dir", func(ctx context.Context) error { stepErr = ctx.Err(); return nil })
	cancel()

	runTurnCleanup(ctx, "req-1", cleanup)

	if stepErr != nil {
		t.Fatalf("the cleanup step saw %v; a stopped turn still owes the box its cleanup", stepErr)
	}
}

func TestRunTurnCleanupLogsAFailureWithTheRequest(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx, cleanup := tools.WithTurnCleanup(context.Background())
	cleanup.Add("dir", func(context.Context) error {
		return errors.New("remove /workspace/mcp-files/req-9: exit 1")
	})

	runTurnCleanup(ctx, "req-9", cleanup)

	for _, want := range []string{"turn cleanup failed", "request_id=req-9", "/workspace/mcp-files/req-9"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q lacks %q", logs.String(), want)
		}
	}
}
