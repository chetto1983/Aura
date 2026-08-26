package mcptools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
)

func TestIdentityBindingMiddlewareAcceptsOnlyItsOAuthSubject(t *testing.T) {
	var calls atomic.Int32
	next := func(context.Context, string, sdkmcp.Request) (sdkmcp.Result, error) {
		calls.Add(1)
		return nil, nil
	}
	middleware := IdentityBindingMiddleware("identity-a")(next)
	request := &sdkmcp.ClientRequest[*sdkmcp.CallToolParams]{Params: &sdkmcp.CallToolParams{Name: "echo"}}

	ownerCtx := identityctx.WithIdentityID(t.Context(), "identity-a")
	if _, err := middleware(ownerCtx, "tools/call", request); err != nil {
		t.Fatalf("owner call: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("owner calls reaching transport = %d", calls.Load())
	}

	foreignCtx := identityctx.WithIdentityID(t.Context(), "identity-b")
	if _, err := middleware(foreignCtx, "tools/call", request); !errors.Is(err, errRemoteIdentityMismatch) {
		t.Fatalf("foreign call error = %v", err)
	}
	if _, err := middleware(t.Context(), "tools/call", request); !errors.Is(err, errMissingRemoteIdentity) {
		t.Fatalf("anonymous call error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("refused calls reached transport; calls = %d", calls.Load())
	}
}

func TestIdentityBindingMiddlewareLeavesStandardMCPMetadataAlone(t *testing.T) {
	called := false
	next := func(context.Context, string, sdkmcp.Request) (sdkmcp.Result, error) {
		called = true
		return nil, nil
	}
	middleware := IdentityBindingMiddleware("identity-a")(next)
	if _, err := middleware(t.Context(), "tools/list", nil); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if !called {
		t.Fatal("non-call method did not reach transport")
	}

	params := &sdkmcp.CallToolParams{Name: "echo"}
	ctx := identityctx.WithIdentityID(t.Context(), "identity-a")
	request := &sdkmcp.ClientRequest[*sdkmcp.CallToolParams]{Params: params}
	if _, err := middleware(ctx, "tools/call", request); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if params.GetMeta() != nil {
		t.Fatalf("identity binding wrote proprietary MCP metadata: %#v", params.GetMeta())
	}
}
