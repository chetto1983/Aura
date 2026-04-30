package tools

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/auth"
)

// fakeSender records SendToUser calls so tests can assert delivery without
// a live Telegram client. Thread-safe because the tool may be invoked
// concurrently in larger fixtures.
type fakeSender struct {
	mu    sync.Mutex
	calls []sendCall
	err   error
}

type sendCall struct {
	userID  string
	message string
}

func (f *fakeSender) SendToUser(userID, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, sendCall{userID, message})
	return nil
}

func newAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	s, err := auth.OpenStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRequestDashboardToken_HappyPath(t *testing.T) {
	store := newAuthStore(t)
	sender := &fakeSender{}
	allow := func(uid string) bool { return uid == "alice" }
	tool := NewRequestDashboardTokenTool(store, sender, allow)
	if tool == nil {
		t.Fatal("tool nil")
	}
	ctx := WithUserID(context.Background(), "alice")
	got, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got == "" {
		t.Error("empty result")
	}
	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.userID != "alice" {
		t.Errorf("recipient = %q, want alice", call.userID)
	}
	if !strings.Contains(call.message, "Dashboard token") {
		t.Errorf("message missing token preamble: %q", call.message)
	}
	// The result string MUST NOT contain the token (avoid leaking into LLM logs).
	for line := range strings.SplitSeq(call.message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Dashboard") || strings.HasPrefix(line, "Keep") || strings.HasPrefix(line, "Use") || strings.HasPrefix(line, "Paste") {
			continue
		}
		// This is the token line. Confirm it's NOT in the tool result.
		if strings.Contains(got, line) {
			t.Errorf("tool result leaks token text: result=%q, token=%q", got, line)
		}
	}
}

func TestRequestDashboardToken_RejectsNoUserContext(t *testing.T) {
	tool := NewRequestDashboardTokenTool(newAuthStore(t), &fakeSender{}, func(string) bool { return true })
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Error("expected error when user context missing")
	}
}

func TestRequestDashboardToken_RejectsNonAllowlisted(t *testing.T) {
	tool := NewRequestDashboardTokenTool(newAuthStore(t), &fakeSender{}, func(string) bool { return false })
	ctx := WithUserID(context.Background(), "bob")
	if _, err := tool.Execute(ctx, nil); err == nil {
		t.Error("expected error for non-allowlisted user")
	}
}

func TestRequestDashboardToken_RevokesOnSendFailure(t *testing.T) {
	store := newAuthStore(t)
	// Sender that always fails — simulates a Telegram outage.
	sender := &fakeSender{err: errSender}
	allow := func(string) bool { return true }
	tool := NewRequestDashboardTokenTool(store, sender, allow)
	ctx := WithUserID(context.Background(), "alice")
	_, err := tool.Execute(ctx, nil)
	if err == nil {
		t.Fatal("expected delivery error")
	}
	// All issued tokens must be revoked when delivery fails. Issue a new
	// token and verify it's the only valid one.
	freshTok, err := store.Issue(ctx, "alice")
	if err != nil {
		t.Fatalf("re-issue: %v", err)
	}
	if uid, err := store.Lookup(ctx, freshTok); err != nil || uid != "alice" {
		t.Errorf("fresh token not valid: uid=%q err=%v", uid, err)
	}
}

var errSender = &dummyErr{msg: "telegram offline"}

type dummyErr struct{ msg string }

func (e *dummyErr) Error() string { return e.msg }

func TestNewRequestDashboardTokenTool_NilArgs(t *testing.T) {
	if tool := NewRequestDashboardTokenTool(nil, &fakeSender{}, func(string) bool { return true }); tool != nil {
		t.Error("expected nil when store is nil")
	}
	if tool := NewRequestDashboardTokenTool(newAuthStore(t), nil, func(string) bool { return true }); tool != nil {
		t.Error("expected nil when sender is nil")
	}
	if tool := NewRequestDashboardTokenTool(newAuthStore(t), &fakeSender{}, nil); tool != nil {
		t.Error("expected nil when allowlist is nil")
	}
}
