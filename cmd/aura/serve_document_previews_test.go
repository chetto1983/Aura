package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestCraftPreviewCancelledBeforeCommandStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (craftPreviewSandbox{}).RunPreview(ctx, usersandbox.BoxHandle{}, usersandbox.ExecRequest{Command: "sleep 30"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled start must preserve cancellation: %v", err)
	}
}
