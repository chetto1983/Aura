package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestParseMCPAddArgsReadsBothShapes(t *testing.T) {
	name, stdio, err := parseMCPAddArgs([]string{"fetch", "--env", "MODE=x", "--box", "--init-timeout", "60", "--", "uvx", "mcp-server-fetch==2026.8.18"})
	if err != nil || name != "fetch" {
		t.Fatalf("stdio: %q, %v", name, err)
	}
	if stdio.Command != "uvx" || !slices.Equal(stdio.Args, []string{"mcp-server-fetch==2026.8.18"}) ||
		!slices.Equal(stdio.Env, []string{"MODE=x"}) || !mcp.IsBoxRuntime(stdio) || stdio.Runtime.InitTimeoutSec != 60 ||
		stdio.Trust.Class != mcp.TrustBlocked || !*stdio.Enabled {
		t.Fatalf("stdio = %#v", stdio)
	}

	_, remote, err := parseMCPAddArgs([]string{"gh", "--disabled", "--url", " https://mcp.example.test/mcp "})
	if err != nil {
		t.Fatalf("remote: %v", err)
	}
	if remote.URL != "https://mcp.example.test/mcp" || remote.Type != mcp.ServerTypeStreamableHTTP ||
		remote.Command != "" || *remote.Enabled || remote.Trust.Class != mcp.TrustBlocked {
		t.Fatalf("remote = %#v", remote)
	}
	if kind, _, err := mcp.Classify(remote); err != nil || kind != mcp.ServerTypeStreamableHTTP {
		t.Fatalf("classify remote = %q, %v", kind, err)
	}
}

func TestParseMCPAddArgsRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{" "}, "name cannot be empty"},
		{[]string{"x"}, "usage:"},
		{[]string{"x", "--"}, "usage:"},
		{[]string{"x", "--env"}, "requires KEY=VALUE"},
		{[]string{"x", "--env", "VALUE"}, "must be KEY=VALUE"},
		{[]string{"x", "--trust", "remote"}, "must be local"},
		{[]string{"x", "--url"}, "--url requires a URL"},
		{[]string{"x", "--bogus"}, "unknown mcp add option"},
		{[]string{"x", "--url", "https://a.test", "--", "node", "s.js"}, "two different servers"},
		{[]string{"x", "--url", "https://a.test", "--box"}, "apply to a stdio server"},
		{[]string{"x", "--url", "https://a.test", "--trust", "local"}, "apply to a stdio server"},
		{[]string{"x", "--url", "https://a.test", "--init-timeout", "60"}, "apply to a stdio server"},
	} {
		if _, _, err := parseMCPAddArgs(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}

// The remote install the cockpit previews as `aura mcp add <name> --url <url>` runs and stores
// the server the cockpit would.
func TestMCPAddURLStoresARemoteServer(t *testing.T) {
	withMemoryMCPRegistry(t)
	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"add", "gh", "--url", "https://mcp.example.test/mcp"}, &out); err != nil {
		t.Fatalf("mcp add --url: %v", err)
	}
	server, ok := readMCPRegistry(t).MCPServers["gh"]
	if !ok || server.URL != "https://mcp.example.test/mcp" || server.Type != mcp.ServerTypeStreamableHTTP {
		t.Fatalf("stored %#v", server)
	}
	if kind, trust, _ := mcp.Classify(server); kind != mcp.ServerTypeStreamableHTTP || trust != mcp.TrustBlocked {
		t.Fatalf("an added URL is %q/%q, want blocked until approved", kind, trust)
	}
	if err := runMCPCommand(context.Background(), nil, []string{"trust", "gh", "--class", mcp.TrustRemoteHTTP, "--reason", "reviewed"}, &out); err != nil {
		t.Fatalf("mcp trust --class remote_http: %v", err)
	}
	if _, trust, err := mcp.Classify(readMCPRegistry(t).MCPServers["gh"]); err != nil || trust != mcp.TrustRemoteHTTP {
		t.Fatalf("after trust: %q, %v", trust, err)
	}
}
