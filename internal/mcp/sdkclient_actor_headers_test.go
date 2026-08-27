package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// startSDKHTTPServerCapturingHeaders is startSDKHTTPServer's shape with one
// addition: every incoming request's headers are captured, most-recent-last,
// so a test can assert what actually reached the wire rather than a client-side
// struct. A second bounded fixture rather than widening startSDKHTTPServer's
// signature for every existing caller.
func startSDKHTTPServerCapturingHeaders(t *testing.T, toolCount int) (*httptest.Server, *sync.Mutex, *[]http.Header) {
	t.Helper()
	srv := newSDKFixtureServer(toolCount, 0)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)
	var mu sync.Mutex
	var seen []http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		for session := range srv.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts, &mu, &seen
}

// TestOpenSDKSessionAppliesSessionHeaders proves SessionOptions.Headers -- the
// seam D-10 (Phase 51) uses to carry a host-derived actor (run id + writer
// role) to cmd/arcadedb-mcp beside the bearer's `sub` -- reaches the wire on a
// real streamable-HTTP session, the same way TestWithStaticHeadersInjectsOnTheWire
// proves it for MCP_HEADER_* env entries. Asserted on what the server actually
// received, never a client-side struct (a wrapper that mutated a copy would
// pass a struct-level assertion and fail on the wire).
func TestOpenSDKSessionAppliesSessionHeaders(t *testing.T) {
	ts, mu, seen := startSDKHTTPServerCapturingHeaders(t, 1)
	server := recipeServer(ts.URL)
	egress := EgressPolicyForManagedServer(true, server)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := OpenSDKSession(ctx, "memory", server, egress, SessionOptions{
		Headers: map[string]string{"X-Aura-Actor-Run-Id": "run-worker-1", "X-Aura-Actor-Role": "worker"},
	})
	if err != nil {
		t.Fatalf("OpenSDKSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	mu.Lock()
	defer mu.Unlock()
	if len(*seen) == 0 {
		t.Fatal("no requests observed by the fixture server")
	}
	last := (*seen)[len(*seen)-1]
	if got := last.Get("X-Aura-Actor-Run-Id"); got != "run-worker-1" {
		t.Errorf("X-Aura-Actor-Run-Id = %q, want %q", got, "run-worker-1")
	}
	if got := last.Get("X-Aura-Actor-Role"); got != "worker" {
		t.Errorf("X-Aura-Actor-Role = %q, want %q", got, "worker")
	}
}

// TestOpenSDKSessionHeaderFuncIsPerRequest proves HeaderFunc is read fresh on
// EACH request, not fixed once at connect time -- the property a REUSED
// identity session needs across many different turns (D-10, Phase 51).
func TestOpenSDKSessionHeaderFuncIsPerRequest(t *testing.T) {
	ts, mu, seen := startSDKHTTPServerCapturingHeaders(t, 1)
	server := recipeServer(ts.URL)
	egress := EgressPolicyForManagedServer(true, server)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var calls int
	session, err := OpenSDKSession(ctx, "memory", server, egress, SessionOptions{
		HeaderFunc: func(context.Context) map[string]string {
			calls++
			return map[string]string{"X-Aura-Actor-Run-Id": "call-" + strconv.Itoa(calls)}
		},
	})
	if err != nil {
		t.Fatalf("OpenSDKSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	// Connect already issued at least one request; make a second real one
	// (ListTools) so two DISTINCT header values must have been observed.
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*seen) < 2 {
		t.Fatalf("want at least 2 requests to prove per-request freshness, got %d", len(*seen))
	}
	values := map[string]bool{}
	for _, h := range *seen {
		if v := h.Get("X-Aura-Actor-Run-Id"); v != "" {
			values[v] = true
		}
	}
	if len(values) < 2 {
		t.Fatalf("HeaderFunc did not vary per request: observed values = %v", values)
	}
}

// TestOpenSDKSessionOmitsActorHeadersWhenAbsent proves an unset Headers field
// adds nothing -- the parent's ordinary session must not gain a stray header
// that a naive always-set implementation would introduce.
func TestOpenSDKSessionOmitsActorHeadersWhenAbsent(t *testing.T) {
	ts, mu, seen := startSDKHTTPServerCapturingHeaders(t, 1)
	server := recipeServer(ts.URL)
	egress := EgressPolicyForManagedServer(true, server)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := OpenSDKSession(ctx, "memory", server, egress, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	mu.Lock()
	defer mu.Unlock()
	if len(*seen) == 0 {
		t.Fatal("no requests observed by the fixture server")
	}
	last := (*seen)[len(*seen)-1]
	if got := last.Get("X-Aura-Actor-Run-Id"); got != "" {
		t.Errorf("X-Aura-Actor-Run-Id = %q, want absent", got)
	}
}
