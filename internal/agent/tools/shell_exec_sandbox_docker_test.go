//go:build docker_integration

// shell_exec_sandbox_docker_test.go is the docker_integration tier for the routed host tools
// (SBX-01, plan 37-07): it proves — against a LIVE Docker daemon — that a
// shell_exec runs INSIDE the per-identity box (the host fs is untouched), and that a snippet
// MaterializeIn'd at /skills/<name>/... is EXECUTED by the routed shell_exec at the SAME SandboxPath
// skill action=use renders (the rendered path and the materialized path agree at RUNTIME, not just
// by construction). Compiled ONLY under `-tags docker_integration`; it skips locally when dockerd is
// unreachable (the Windows worktree) and t.Fatals under $CI (no-skip-as-green).

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/testenv"
	"github.com/moby/moby/client"
)

func dockerTestImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE")); v != "" {
		return v
	}
	return "python:3-slim"
}

func newDockerRouter(t *testing.T, sources usersandbox.SourceResolver) *usersandbox.SandboxRouter {
	t.Helper()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	// Without this the client's keep-alive persistConn goroutines outlive the test and the
	// package's goleak TestMain fails the WHOLE tier even when every test passes — so the
	// docker_integration tier could never go green. The sibling usersandbox helper
	// (docker_backend_integration_test.go) has always closed its client; this one did not.
	t.Cleanup(func() { _ = cli.Close() })
	limits := usersandbox.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 1 << 30, PidsLimit: 256}
	opts := []usersandbox.Option{}
	if sources != nil {
		opts = append(opts, usersandbox.WithMaterializeSources(sources))
	}
	backend := usersandbox.NewDockerBackend(cli, dockerTestImage(), limits, opts...)
	router := usersandbox.NewSandboxRouter(backend, config.SandboxConfig{
		Image: dockerTestImage(), CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256, IdleTTLSec: 1800,
	})
	// Every test here routes with no principal, into `local`'s box: destroy it, or the next test
	// inherits its files.
	t.Cleanup(func() { _ = router.Destroy(context.Background(), identityctx.LocalOperatorIdentity) })
	return router
}

// TestRoute_StrictExecInBox proves a strict-profile shell_exec runs inside the box: a file written
// under /workspace is readable back IN the box, and the host cwd is never touched.
func TestRoute_StrictExecInBox(t *testing.T) {
	testenv.Dockerd(t)
	router := newDockerRouter(t, nil)
	tool := &ShellExec{Router: router}
	ctx := ctxWith(t, "sess-dk-exec", "call-dk-exec")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo BOXHELLO > /workspace/f.txt && cat /workspace/f.txt && whoami"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "BOXHELLO") {
		t.Fatalf("box exec did not run inside the box: %q", res.Preview)
	}
	// The host must NOT have a /workspace/f.txt written by the box (storage isolation).
	if _, herr := os.Stat("/workspace/f.txt"); herr == nil {
		t.Fatal("box write leaked to the host /workspace — storage isolation breached")
	}
}

// TestSnippetExec_RoutedEndToEnd proves the rendered SandboxPath and the MaterializeIn destination
// agree at RUNTIME: a snippet materialized at /skills/calc/calc.py is executed by the routed
// shell_exec at the exact path skill action=use rendered, returning the snippet's stdout.
func TestSnippetExec_RoutedEndToEnd(t *testing.T) {
	testenv.Dockerd(t)

	// Host-side snippet the box materializes at /skills/calc/calc.py.
	exportDir := t.TempDir()
	calcDir := filepath.Join(exportDir, "calc")
	if err := os.MkdirAll(calcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(calcDir, "calc.py"), []byte("print('SNIPPET-OK')\n"), 0o644); err != nil {
		t.Fatalf("write snippet: %v", err)
	}
	sources := func(context.Context, string) ([]usersandbox.MaterializeSource, error) {
		return []usersandbox.MaterializeSource{{HostDir: exportDir, Dest: "/skills"}}, nil
	}
	router := newDockerRouter(t, sources)

	// skill action=use (strict) renders the SandboxPath the box materialized the snippet at.
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {instructions: "Add.", sandboxPath: "/skills/calc/calc.py", interpreter: "python3"},
	}
	skill := &SkillTool{Loader: loader}
	ctx := ctxWith(t, "sess-dk-snip", "call-dk-snip")
	use, err := skill.Execute(ctx, json.RawMessage(`{"action":"use","name":"calc"}`))
	if err != nil {
		t.Fatalf("skill use: %v", err)
	}
	if !strings.Contains(use.Preview, "/skills/calc/calc.py") {
		t.Fatalf("action=use must render the sandbox path: %q", use.Preview)
	}

	// The routed shell_exec runs the snippet at the rendered path INSIDE the box.
	sh := &ShellExec{Router: router}
	run, err := sh.Execute(ctx, json.RawMessage(`{"command":"python3 /skills/calc/calc.py"}`))
	if err != nil {
		t.Fatalf("routed snippet exec: %v", err)
	}
	if !strings.Contains(run.Preview, "SNIPPET-OK") {
		t.Fatalf("materialized snippet did not execute at the rendered SandboxPath: %q", run.Preview)
	}
}
