package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
)

// --box declares the box runtime and hands the install guard a way into the operator's box, so
// the handshake a CLI add requires happens where the server will run, never on this host.
func TestMCPAddBoxDeclaresTheRuntimeAndVerifiesInTheOperatorsBox(t *testing.T) {
	withMemoryMCPRegistry(t)
	prev := mcpInstallGuard
	t.Cleanup(func() { mcpInstallGuard = prev })
	var gotBox mcp.BoxLauncher
	var gotOwner string
	mcpInstallGuard = func(ctx context.Context, _ *mcpenv.Preparer, _ string, s mcp.ManagedServer, box mcp.BoxLauncher) (mcp.ManagedServer, mcpenv.Report, *mcp.ProbeResult, error) {
		gotBox, gotOwner = box, identityctx.IdentityID(ctx)
		return s, mcpenv.Report{}, nil, nil
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"add", "fetch", "--box", "--init-timeout", "120", "--", "uvx", "mcp-server-fetch==2026.8.18"}, &out); err != nil {
		t.Fatalf("mcp add --box: %v", err)
	}
	server := readMCPRegistry(t).MCPServers["fetch"]
	if !mcp.IsBoxRuntime(server) || server.Runtime.InitTimeoutSec != 120 || server.Trust.Class != mcp.TrustBlocked {
		t.Fatalf("stored %+v, want a box server that still waits for approval", server)
	}
	if gotBox == nil || gotOwner != identityctx.LocalOperatorIdentity {
		t.Fatalf("guard got box=%v owner=%q, want the operator's box", gotBox, gotOwner)
	}
}

func TestMCPAddInitTimeoutNeedsSeconds(t *testing.T) {
	for _, args := range [][]string{
		{"add", "fetch", "--box", "--init-timeout", "soon", "--", "uvx", "mcp-server-fetch==2026.8.18"},
		{"add", "fetch", "--box", "--init-timeout"},
	} {
		if err := runMCPCommand(context.Background(), nil, args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--init-timeout") {
			t.Fatalf("%q: err = %v, want an --init-timeout refusal", args, err)
		}
	}
}
