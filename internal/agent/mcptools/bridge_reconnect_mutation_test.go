package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestReconnectBackoff_Schedule pins the exact exponential-backoff schedule:
// failures<=0 → 0, then 500ms doubling each failure, capped at the 30s ceiling.
// Asserting the precise per-step value (not just monotonicity) is what catches
// off-by-one loop-bound and dropped-return mutants in reconnectBackoff.
func TestReconnectBackoff_Schedule(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{-1, 0},
		{0, 0},
		{1, mcpReconnectBackoffMin},
		{2, 1 * time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 8 * time.Second},
		{6, 16 * time.Second},
		{7, mcpReconnectBackoffMax},
		{8, mcpReconnectBackoffMax},
		{50, mcpReconnectBackoffMax},
	}
	for _, c := range cases {
		if got := reconnectBackoff(c.failures); got != c.want {
			t.Errorf("reconnectBackoff(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

// TestReconnectServer_CurrentClientNilWhileOpen covers currentClient's nil-client
// guard independently of the closed flag: an open server with no client must error
// rather than hand back a nil client (which would nil-panic the caller).
func TestReconnectServer_CurrentClientNilWhileOpen(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, nil)
	_, err := srv.currentClient()
	if err == nil {
		t.Fatal("currentClient with a nil client (server not closed) must return an error")
	}
	if !mcp.IsTransportError(err) {
		t.Fatalf("want a transport error for a missing client, got %v", err)
	}
}

// TestReconnectServer_BreakerErrorElapsedReopens pins the breaker gate's three
// states: zero (never tripped) and elapsed (cooldown passed) both allow a retry
// (nil), while a still-open future window blocks with an error.
func TestReconnectServer_BreakerErrorElapsedReopens(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &errReconnectClient{})
	now := time.Now()

	if err := srv.reconnectBreakerErrorLocked(now); err != nil {
		t.Fatalf("a zero breaker must not block, got %v", err)
	}

	srv.breakerOpenUntil = now.Add(time.Hour)
	if err := srv.reconnectBreakerErrorLocked(now); err == nil {
		t.Fatal("a breaker open in the future must block with an error")
	}

	srv.breakerOpenUntil = now.Add(-time.Hour)
	if err := srv.reconnectBreakerErrorLocked(now); err != nil {
		t.Fatalf("an elapsed breaker (cooldown passed) must reopen (nil), got %v", err)
	}
}

// TestReconnectServer_SuccessfulReconnectResetsState drives a real transport-error
// reconnect that succeeds and asserts the success branch's observable effects: the
// fresh client is swapped in, a prior failure count is reset to zero, and the failed
// client is closed.
func TestReconnectServer_SuccessfulReconnectResetsState(t *testing.T) {
	initial := &errReconnectClient{
		listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)},
	}
	fresh := &fakeReconnectClient{defs: []mcp.ToolDef{{Name: "fresh", Description: "Fresh."}}}
	restore := stubOpenMCPClient(t, fresh)
	defer restore()

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	srv.mu.Lock()
	srv.reconnectFailures = 2
	srv.mu.Unlock()

	defs, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools after reconnect: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "fresh" {
		t.Fatalf("want fresh defs from the reopened client, got %v", defs)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.client != fresh {
		t.Fatal("a successful reconnect must swap the fresh client into s.client")
	}
	if srv.reconnectFailures != 0 {
		t.Fatalf("a successful reconnect must reset reconnectFailures to 0, got %d", srv.reconnectFailures)
	}
	if !initial.closed {
		t.Fatal("the superseded client must be closed after a successful reconnect")
	}
}

// TestReconnectServer_RefreshHookFiresOnChangedSpec covers refreshSpecsLocked's
// change-detection + hook dispatch: when a reopened spec updates a bridged tool the
// stored refresh hook must fire exactly once. Catches dropped change-flag, dropped
// hook-store (setRefreshHook), and dropped hook-call mutants.
func TestReconnectServer_RefreshHookFiresOnChangedSpec(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &errReconnectClient{})

	bt := &bridgedTool{name: "send"}
	bt.storeSpec(tools.Spec{Name: "mail.send", Description: "old", Parameters: json.RawMessage(`{"type":"object"}`)})
	srv.bridged["send"] = bt

	called := 0
	srv.setRefreshHook(func() { called++ })

	srv.mu.Lock()
	srv.refreshSpecsLocked([]mcp.ToolDef{{Name: "send", Description: "new"}})
	srv.mu.Unlock()

	if called != 1 {
		t.Fatalf("refresh hook must fire once when a bridged spec changed, fired %d", called)
	}
}

// TestReconnectServer_AlreadyClosedCloseReleasesLock covers the lock-release on
// Close's already-closed early-return branch: a dropped Unlock there strands the
// mutex, so a follow-up lock-taking call would deadlock. The timeout guard turns
// that deadlock into a fast, deterministic failure.
func TestReconnectServer_AlreadyClosedCloseReleasesLock(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &errReconnectClient{})
	if err := srv.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second (already-closed) Close: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = srv.currentClient()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("currentClient deadlocked — the already-closed Close branch did not release the lock")
	}
}
