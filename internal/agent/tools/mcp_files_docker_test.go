//go:build docker_integration

// mcp_files_docker_test.go is the docker_integration proof for MCPFileSink: against a
// live box, a file an MCP result carried lands at the path the model is told, with the
// bytes it came with, and is gone once the turn's cleanup has run; and a turn directory
// an unclean exit left behind is gone after the next write.

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
	// newDockerRouter selects gVisor, which the CI daemon lacks; it would only pass by reusing
	// the runc box another test created first.
	router := newRuncBoxRouter(t)
	ctx, cleanup := WithTurnCleanup(WithRequestID(ctxWith(t, "sess-dk-mcp", "call-dk-mcp"), "req-dk-mcp"))
	// A failure midway must not leave the file in the volume: the next run would name its
	// file "Fattura è-2.pdf". Run forgets its steps, so this does nothing after the asserted Run.
	t.Cleanup(func() { _ = cleanup.Run(context.Background()) })

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

// A process that died mid-turn never ran its turn's removal, so its directory stays in the
// volume: the next call's write, of any turn, sweeps it once it is a day old.
func TestMCPFileSink_SweepsATurnDirectoryAnUncleanExitLeftBehind(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newRuncBoxRouter(t)
	ctx, cleanup := WithTurnCleanup(WithRequestID(ctxWith(t, "sess-dk-sweep", "call-dk-sweep"), "req-dk-sweep"))
	// Run forgets its steps, so this removes the new turn's directory once and only once.
	t.Cleanup(func() { _ = cleanup.Run(context.Background()) })
	handle, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	const orphan = "/workspace/mcp-files/req-old-orphan"
	t.Cleanup(func() {
		_, _ = router.Exec(context.Background(), handle, usersandbox.ExecRequest{Command: "rm -rf -- " + ShellQuoteArg(orphan)})
	})
	// touch goes last: writing into the directory would give it a fresh mtime.
	seed := "mkdir -p " + orphan + "/aura-pim && echo left > " + orphan + "/aura-pim/left.pdf && touch -d '2 days ago' " + orphan
	if res, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: seed}); err != nil || res.ExitCode != 0 {
		t.Fatalf("seeding the orphan: exit %d, stderr %q (err %v)", res.ExitCode, res.Stderr, err)
	}

	out := (&MCPFileSink{Router: router}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "new.pdf", MIMEType: "application/pdf", Data: []byte("%PDF-1.7 sweep")},
	})
	if out[0].Path != "/workspace/mcp-files/req-dk-sweep/aura-pim/new.pdf" {
		t.Fatalf("outcome = %+v", out[0])
	}

	gone, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "test -e " + orphan + " && echo LEFT || echo GONE"})
	if err != nil || strings.TrimSpace(string(gone.Stdout)) != "GONE" {
		t.Fatalf("the two-day-old orphan is %q (err %v) after a materialize, want GONE", gone.Stdout, err)
	}
	read, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "cat -- " + ShellQuoteArg(out[0].Path)})
	if err != nil || string(read.Stdout) != "%PDF-1.7 sweep" {
		t.Fatalf("the sweep must not touch the turn's own file: the box holds %q (err %v)", read.Stdout, err)
	}
}
