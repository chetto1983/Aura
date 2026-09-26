package main

import (
	"context"
	"strings"
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

// The install command must stay a no-op once its record matches, serialise concurrent starts,
// leave no record behind a failure, and keep a comment in the script from eating the subshell.
func TestBoxInstallCommandRecordsLocksAndSurfacesFailure(t *testing.T) {
	got := boxInstallCommand("my calc", "pip install calc # pinned")
	for _, want := range []string{
		"mkdir -p '/workspace/.aura-mcp'",
		"flock '/workspace/.aura-mcp/my_calc.installed.lock' sh -c",
		"/workspace/.aura-mcp/my_calc.installed",
		"pip install calc # pinned\n)",
		"/tmp/aura-mcp-my_calc-install.log",
		"tail -n 20",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("install command lacks %q:\n%s", want, got)
		}
	}
	if boxInstallCommand("c", "a") == boxInstallCommand("c", "b") {
		t.Fatal("two different scripts share one record hash, so a changed install would never run")
	}
}
