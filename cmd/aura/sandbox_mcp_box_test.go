package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

func TestBoxStdioCommandQuotesArgvAndKeepsStderrOffTheProtocol(t *testing.T) {
	got := boxStdioCommand("my server/../x", []string{"agent-browser", "mcp", "it's"})
	want := `'agent-browser' 'mcp' 'it'\''s' 2>>'/tmp/aura-mcp-my_server____x.log'`
	if got != want {
		t.Fatalf("boxStdioCommand = %s\nwant             %s", got, want)
	}
}

func TestNoSandboxMeansNoBoxLauncher(t *testing.T) {
	if newSandboxMCPBox(nil) != nil {
		t.Fatal("a host with no sandbox router got a box launcher, so box servers would fail late instead of being refused")
	}
}

func TestBoxDiscoveryWithoutADatabaseUsesTheRoutersOwnIdentity(t *testing.T) {
	if got := boxDiscoveryIdentity(context.Background(), nil); got != identityctx.LocalOperatorIdentity {
		t.Fatalf("boxDiscoveryIdentity(nil pool) = %q", got)
	}
}
