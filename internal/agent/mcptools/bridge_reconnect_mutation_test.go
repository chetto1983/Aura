package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
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

func TestReconnectServer_MutatingOperationDoesNotReconnectOrReplay(t *testing.T) {
	initial := &fakeReconnectClient{callErr: mcp.ErrTransport}
	fresh := &fakeReconnectClient{defs: []mcp.ToolDef{{Name: "send"}}}
	oldOpen := openMCPClient
	var reconnects int
	openMCPClient = func(context.Context, context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		reconnects++
		return fresh, nil
	}
	defer func() { openMCPClient = oldOpen }()

	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeMCPTool, Key: "mcp-mutation"},
		Fingerprint: [32]byte{1},
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	if _, err := srv.CallTool(ctx, "send", map[string]any{"message": "hello"}); !mcp.IsTransportError(err) {
		t.Fatalf("error = %v, want terminal transport ambiguity", err)
	}
	if initial.callCount != 1 || fresh.callCount != 0 || reconnects != 0 {
		t.Fatalf("calls/reconnects = initial:%d fresh:%d reconnects:%d, want 1/0/0", initial.callCount, fresh.callCount, reconnects)
	}
}

func TestReconnectServer_ReadOnlyToolReconnectsAndReissues(t *testing.T) {
	initial := &fakeReconnectClient{callErr: mcp.ErrTransport}
	fresh := &fakeReconnectClient{defs: []mcp.ToolDef{{Name: "lookup", Annotations: mcp.ToolAnnotations{ReadOnlyHint: true}}}, callText: "found"}
	restore := stubOpenMCPClient(t, fresh)
	defer restore()

	srv := newReconnectingServer("catalog", mcp.ServerConfig{Command: "fake"}, initial)
	bridged := bridgeTools("catalog", srv, []mcp.ToolDef{{Name: "lookup", Annotations: mcp.ToolAnnotations{ReadOnlyHint: true}}}, time.Second)
	srv.trackBridgedTools(bridged)
	text, err := srv.CallTool(context.Background(), "lookup", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if text != "found" || initial.callCount != 1 || fresh.callCount != 1 {
		t.Fatalf("text/calls = %q initial:%d fresh:%d, want found/1/1", text, initial.callCount, fresh.callCount)
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

func TestReconnectServerRejectsToolSetDrift(t *testing.T) {
	t.Parallel()
	srv := newReconnectingServer("mail", mcp.ServerConfig{}, &errReconnectClient{})
	srv.trackAcceptedDefs([]mcp.ToolDef{{Name: "send"}, {Name: "list"}})

	if err := srv.validateReconnectToolSet([]mcp.ToolDef{{Name: "list"}, {Name: "send"}}); err != nil {
		t.Fatalf("same tool set rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		defs []mcp.ToolDef
	}{
		{"added", []mcp.ToolDef{{Name: "list"}, {Name: "send"}, {Name: "archive"}}},
		{"removed", []mcp.ToolDef{{Name: "send"}}},
		{"renamed", []mcp.ToolDef{{Name: "list"}, {Name: "deliver"}}},
		{"duplicate", []mcp.ToolDef{{Name: "list"}, {Name: "list"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := srv.validateReconnectToolSet(tc.defs); err == nil ||
				!strings.Contains(err.Error(), "tool set changed") {
				t.Fatalf("drift error = %v, want restart-required tool-set error", err)
			}
		})
	}
}

func TestReconnectServerSuppressesReplacementAfterToolSetDrift(t *testing.T) {
	initial := &errReconnectClient{
		listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)},
	}
	fresh := &fakeReconnectClient{defs: []mcp.ToolDef{{Name: "send"}, {Name: "archive"}}}
	restore := stubOpenMCPClient(t, fresh)
	defer restore()

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	srv.trackAcceptedDefs([]mcp.ToolDef{{Name: "send"}})

	if _, err := srv.ListTools(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "restart required") {
		t.Fatalf("drift reconnect error = %v, want restart required", err)
	}
	if !fresh.closed {
		t.Fatal("drifted replacement client was not closed")
	}
	if _, err := srv.CallTool(context.Background(), "send", nil); err == nil ||
		!strings.Contains(err.Error(), "restart required") {
		t.Fatalf("stale bridged call error = %v, want suppressed server", err)
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
