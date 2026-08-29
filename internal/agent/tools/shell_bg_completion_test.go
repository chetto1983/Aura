package tools

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

func completionTestShell(t *testing.T, status string) (*bgShell, <-chan BackgroundShellCompletion) {
	t.Helper()
	bg := NewBackgroundShells(nil)
	got := make(chan BackgroundShellCompletion, 2)
	bg.SetCompletionHook(func(completion BackgroundShellCompletion) { got <- completion })
	ctx := identityctx.WithIdentityID(context.Background(), "owner-completion")
	ctx = WithToolCallContext(ctx, "conversation-completion", "call-completion", "", 0)
	sh := bg.newShell(ctx, "shell-completion")
	sh.startedAt = time.Now().Add(-25 * time.Millisecond)
	switch status {
	case "killed":
		sh.killed = true
	case "expired":
		sh.expired = true
	}
	return sh, got
}

func TestBackgroundShellCompletionHookTerminalStatesExactlyOnce(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		wait       func() error
		wantStatus string
	}{
		{name: "normal exit", wait: func() error { return &bgBoxExit{code: 0} }, wantStatus: "exited:0"},
		{name: "killed", state: "killed", wait: func() error { return nil }, wantStatus: "killed"},
		{name: "expired", state: "expired", wait: func() error { return nil }, wantStatus: "expired"},
		{name: "wait panic", wait: func() error { panic("completion panic") }, wantStatus: "exited:1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh, got := completionTestShell(t, tc.state)
			runBackgroundShellReaper(sh, tc.wait, func() {})

			select {
			case completion := <-got:
				if completion.ShellID != "shell-completion" || completion.OwnerID != "owner-completion" || completion.SessionID != "conversation-completion" {
					t.Fatalf("completion routing = %+v", completion)
				}
				if completion.Status != tc.wantStatus {
					t.Fatalf("status = %q, want %q", completion.Status, tc.wantStatus)
				}
				if completion.Duration <= 0 {
					t.Fatalf("duration = %v, want positive", completion.Duration)
				}
			case <-time.After(time.Second):
				t.Fatal("completion hook did not fire")
			}

			sh.notifyCompletion()
			select {
			case duplicate := <-got:
				t.Fatalf("completion hook fired twice: %+v", duplicate)
			default:
			}
		})
	}
}

func TestBackgroundShellCompletionHookPanicDoesNotRewriteExit(t *testing.T) {
	bg := NewBackgroundShells(nil)
	bg.SetCompletionHook(func(BackgroundShellCompletion) { panic("composition hook") })
	sh := bg.newShell(context.Background(), "shell-hook-panic")
	runBackgroundShellReaper(sh, func() error { return &bgBoxExit{code: 7} }, func() {})

	_, status := sh.snapshot(nil)
	if status != "exited:7" {
		t.Fatalf("status = %q, want exited:7 after hook panic", status)
	}
}
