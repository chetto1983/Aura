package telegram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation"
)

func TestStopLogsArchiverCloseFailure(t *testing.T) {
	var logs bytes.Buffer
	b := &Bot{
		logger:   slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		archiver: &closeFailingArchiver{err: errors.New("close boom")},
	}

	b.Stop()

	got := logs.String()
	for _, want := range []string{"telegram shutdown: archiver close failed", `error="close boom"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}

type closeFailingArchiver struct {
	err error
}

func (f *closeFailingArchiver) Append(context.Context, conversation.Turn) error {
	return nil
}

func (f *closeFailingArchiver) Close(context.Context) error {
	return f.err
}
