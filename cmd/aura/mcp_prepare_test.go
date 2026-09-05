package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
)

// withoutMCPInstallGuard neutralises the amendment #211 install guard for the tests that
// exercise registry bookkeeping (add/list/disable/trust/status) rather than launchability.
// Those tests declare fixture commands that were never meant to start a real MCP server, and
// before #211 an add stored whatever it was given. The guard's own behaviour is covered by
// TestPrepareAndVerify* and by internal/mcp/mcpenv.
func withoutMCPInstallGuard(t *testing.T) {
	t.Helper()
	prev := mcpInstallGuard
	mcpInstallGuard = func(_ context.Context, _ *mcpenv.Preparer, _ string, s mcp.ManagedServer) (mcp.ManagedServer, mcpenv.Report, error) {
		return s, mcpenv.Report{}, nil
	}
	t.Cleanup(func() { mcpInstallGuard = prev })
}

// An HTTP server has no environment to prepare and keeps the post-save probe it always had,
// so the guard must leave it exactly as it found it — including not spawning anything.
func TestPrepareAndVerifyLeavesHTTPServersAlone(t *testing.T) {
	in := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.example.test"}
	out, rep, err := prepareAndVerify(context.Background(), nil, "remote", in)
	if err != nil {
		t.Fatalf("prepareAndVerify: %v", err)
	}
	if rep.Prepared {
		t.Fatalf("report = %#v, want nothing prepared for an HTTP server", rep)
	}
	if out.URL != in.URL || out.Command != "" {
		t.Fatalf("server rewritten: %#v", out)
	}
}

// The whole point of #211: a stdio server that cannot complete a handshake is NOT stored. The
// error has to name the server and say it was not installed, because the failure this replaces
// reported "recv: unexpected EOF" and sent the operator looking at the transport.
func TestPrepareAndVerifyRefusesAServerThatCannotHandshake(t *testing.T) {
	in := mcp.ManagedServer{Command: "/nonexistent/aura-test-mcp-server", Args: []string{"--stdio"}}
	_, _, err := prepareAndVerify(context.Background(), nil, "broken", in)
	if err == nil {
		t.Fatal("prepareAndVerify accepted a server that cannot start")
	}
	for _, want := range []string{"broken", "not installed", "handshake"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err %q should contain %q", err, want)
		}
	}
}

// A passthrough is a normal outcome and has to say so: silence would read as "prepared".
func TestDescribePreparationDistinguishesPassthrough(t *testing.T) {
	if got := describePreparation(mcpenv.Report{}); !strings.Contains(got, "no environment prepared") {
		t.Fatalf("passthrough description = %q", got)
	}
	got := describePreparation(mcpenv.Report{
		Prepared: true, Ecosystem: "python", Dir: "/var/lib/aura/mcp-envs/calc",
		Package: "calculator-mcp-server", Entrypoint: "/var/lib/aura/mcp-envs/calc/venv/bin/calculator-mcp-server",
	})
	for _, want := range []string{"python", "/var/lib/aura/mcp-envs/calc", "calculator-mcp-server"} {
		if !strings.Contains(got, want) {
			t.Fatalf("description %q should contain %q", got, want)
		}
	}
}

// The add path must surface the refusal rather than storing the row and reporting success.
func TestMCPAddRefusesAnUnverifiableServer(t *testing.T) {
	withMemoryMCPRegistry(t)
	var out bytes.Buffer
	err := runMCPCommand(context.Background(), nil,
		[]string{"add", "broken", "--", "/nonexistent/aura-test-mcp-server"}, &out)
	if err == nil {
		t.Fatal("mcp add stored a server that cannot start")
	}
	if doc := readMCPRegistry(t); len(doc.MCPServers) != 0 {
		t.Fatalf("registry = %#v, want the refused server absent", doc.MCPServers)
	}
	if strings.Contains(out.String(), "ok: added") {
		t.Fatalf("output claimed success: %q", out.String())
	}
}
