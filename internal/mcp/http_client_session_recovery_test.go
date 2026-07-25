package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// sidecarRestartServer models what the memory sidecar does across a restart: the session the
// client holds is gone (404 "Session not found"), and any later request that arrives with NO
// session header is refused with 400 "Bad Request: Missing session ID". Both are FastMCP's
// real answers, captured from the running sidecar on 2026-07-25.
type sidecarRestartServer struct {
	initializes atomic.Int64
	live        atomic.Value // string: the session id the server currently accepts
	toolCalls   atomic.Int64
}

func newSidecarRestartServer(t *testing.T) (*httptest.Server, *sidecarRestartServer) {
	t.Helper()
	state := &sidecarRestartServer{}
	state.live.Store("session-before-restart")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.Method == "initialize" {
			id := fmt.Sprintf("session-%d", state.initializes.Add(1))
			state.live.Store(id)
			w.Header().Set("Mcp-Session-Id", id)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{}}}`, req.ID)
			return
		}

		switch presented := r.Header.Get("Mcp-Session-Id"); {
		case presented == "":
			// The state the client got itself stuck in: no session, so it cannot even ask.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"server-error","error":{"code":-32600,"message":"Bad Request: Missing session ID"}}`))
		case presented != state.live.Load().(string):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"server-error","error":{"code":-32600,"message":"Session not found"}}`))
		default:
			// Count tool calls only — each handshake also posts notifications/initialized
			// on the live session, and counting those would report handshakes as work done.
			if req.Method == "tools/call" {
				state.toolCalls.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"ok"}]}}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// TestSessionRecoversAfterARefusedMutatingRetry reproduces the live lock-up: rebuilding the
// memory sidecar left the running daemon answering http 400 to EVERY memory call — twenty in a
// row — until aura itself was restarted.
//
// The sequence that bricks it: the first call after the restart gets 404, which clears the
// stored session id; because that call is a mutating tool call it must NOT be replayed, so the
// client declines to reinitialize. From then on it posts with no session at all, the server
// answers 400, and the only trigger for reinitialize was the session id already cleared. The
// connection can never recover on its own.
func TestSessionRecoversAfterARefusedMutatingRetry(t *testing.T) {
	t.Parallel()

	srv, state := newSidecarRestartServer(t)
	ctx := context.Background()
	client, err := OpenHTTP(ctx, "memory", HTTPConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// The sidecar restarts: the session the client holds is no longer the live one.
	state.live.Store("session-after-restart")

	mutating, err := idempotency.WithOperation(
		identityctx.WithIdentityID(ctx, identityctx.LocalOperatorIdentity),
		idempotency.Operation{
			Key: idempotency.OperationKey{
				IdentityID: identityctx.LocalOperatorIdentity,
				Scope:      idempotency.ScopeMCPTool,
				Key:        "mutating-call-across-a-sidecar-restart",
			},
			Fingerprint: [32]byte{1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CallTool(mutating, "memory_add_entity", map[string]any{}); err == nil {
		t.Fatal("a mutating call whose session died must not be silently replayed")
	}

	// The next call is a plain read. Before the fix this returned http 400, and so did every
	// call after it for the life of the process.
	if _, err := client.CallTool(ctx, "memory_search", map[string]any{}); err != nil {
		t.Fatalf("client did not recover its session: %v", err)
	}
	if got := state.toolCalls.Load(); got != 1 {
		t.Fatalf("tool calls that reached the server = %d, want 1", got)
	}
	if got := state.initializes.Load(); got != 2 {
		t.Fatalf("initializes = %d, want 2 (open + one recovery)", got)
	}
}
