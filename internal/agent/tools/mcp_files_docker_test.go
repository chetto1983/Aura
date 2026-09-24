//go:build docker_integration

// mcp_files_docker_test.go is the docker_integration proof for MCPFileSink: against a
// live box, a file an MCP result carried lands at the path the model is told, with the
// bytes it came with, and is gone once the turn's cleanup has run.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestMCPFileSink_AFileLivesForItsTurnInARealBox(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newDockerRouter(t, nil)
	ctx, cleanup := WithTurnCleanup(WithRequestID(ctxWith(t, "sess-dk-mcp", "call-dk-mcp"), "req-dk-mcp"))

	out := (&MCPFileSink{Router: router}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "Fattura è.pdf", MIMEType: "application/pdf", Data: []byte("%PDF-1.7 docker")},
	})
	if out[0].Path != "/workspace/mcp-files/req-dk-mcp/aura-pim/Fattura è.pdf" {
		t.Fatalf("outcome = %+v", out[0])
	}

	handle, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	read, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "cat -- " + ShellQuoteArg(out[0].Path)})
	if err != nil || string(read.Stdout) != "%PDF-1.7 docker" {
		t.Fatalf("the box holds %q (err %v), want the file's bytes", read.Stdout, err)
	}

	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("turn cleanup: %v", err)
	}
	gone, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "test -e /workspace/mcp-files/req-dk-mcp && echo LEFT || echo GONE"})
	if err != nil || strings.TrimSpace(string(gone.Stdout)) != "GONE" {
		t.Fatalf("after cleanup the turn directory is %q (err %v), want GONE", gone.Stdout, err)
	}
}
