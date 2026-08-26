package mcptools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestIdentitySessionPoolNeverSharesAnOAuthSession(t *testing.T) {
	var mu sync.Mutex
	opens := map[string]int{}
	var serverSessions []*sdkmcp.ServerSession

	connect := func(_ context.Context, hctx context.Context, options mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		owner := identityctx.IdentityID(hctx)
		mu.Lock()
		opens[owner]++
		mu.Unlock()

		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(&sdkmcp.Tool{Name: "whoami", InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: owner}}}, nil
			})
		clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(hctx, serverTransport, nil)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		serverSessions = append(serverSessions, serverSession)
		mu.Unlock()
		options.Sending = []sdkmcp.Middleware{IdentityBindingMiddleware(owner)}
		return connectClient(hctx, clientTransport, options)
	}

	parent := NewMountedServer("remote", nil)
	pool := newIdentitySessionPool(parent, connect, t.Context())
	parent.identityPool = pool
	ctxA := identityctx.WithIdentityID(t.Context(), "identity-a")
	_, advertised, err := pool.openInitial(ctxA)
	if err != nil {
		t.Fatalf("open initial: %v", err)
	}
	if _, err := bridgeFromAdvertisedWithPolicy("remote", parent, advertised, bridgePolicy{identityScoped: true}); err != nil {
		t.Fatalf("bridge: %v", err)
	}
	parent.trackAcceptedTools(advertised)
	t.Cleanup(func() {
		_ = parent.Close()
		mu.Lock()
		sessions := append([]*sdkmcp.ServerSession(nil), serverSessions...)
		mu.Unlock()
		for _, session := range sessions {
			_ = session.Close()
		}
	})

	for _, tc := range []struct{ identity, want string }{
		{identity: "identity-a", want: "identity-a"},
		{identity: "identity-b", want: "identity-b"},
		{identity: "identity-a", want: "identity-a"},
		{identity: "identity-b", want: "identity-b"},
	} {
		ctx := identityctx.WithIdentityID(t.Context(), tc.identity)
		payload, err := parent.CallTool(ctx, "whoami", nil)
		if err != nil {
			t.Fatalf("CallTool(%s): %v", tc.identity, err)
		}
		if payload.Text != tc.want {
			t.Fatalf("CallTool(%s) = %q", tc.identity, payload.Text)
		}
	}

	const parallelCalls = 12
	start := make(chan struct{})
	errs := make(chan error, parallelCalls)
	for range parallelCalls {
		go func() {
			<-start
			ctx := identityctx.WithIdentityID(t.Context(), "identity-c")
			payload, err := parent.CallTool(ctx, "whoami", nil)
			if err == nil && payload.Text != "identity-c" {
				err = fmt.Errorf("parallel CallTool = %q", payload.Text)
			}
			errs <- err
		}()
	}
	close(start)
	for range parallelCalls {
		if err := <-errs; err != nil {
			t.Fatalf("parallel CallTool: %v", err)
		}
	}

	mu.Lock()
	opensA, opensB, opensC, subjectCount := opens["identity-a"], opens["identity-b"], opens["identity-c"], len(opens)
	mu.Unlock()
	if opensA != 1 || opensB != 1 || opensC != 1 {
		t.Fatalf("session opens = %v, want one per identity", opens)
	}
	if subjectCount != 3 {
		t.Fatalf("unexpected subject sessions: %v", opens)
	}
}

func TestIdentitySessionPoolRejectsAnonymousCalls(t *testing.T) {
	parent := NewMountedServer("remote", nil)
	parent.identityPool = newIdentitySessionPool(parent,
		func(context.Context, context.Context, mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
			return nil, fmt.Errorf("must not open")
		}, t.Context())
	if _, err := parent.CallTool(t.Context(), "whoami", nil); !errors.Is(err, errMissingRemoteIdentity) {
		t.Fatalf("anonymous call error = %v", err)
	}
}
