//go:build docker_integration

// shell_exec_sandbox_docker_test.go is the docker_integration tier for the routed host tools
// (SBX-01, plan 37-07): it proves — against a LIVE Docker daemon — that under a strict profile a
// shell_exec runs INSIDE the per-identity box (the host fs is untouched), and that a snippet
// MaterializeIn'd at /skills/<name>/... is EXECUTED by the routed shell_exec at the SAME SandboxPath
// skill action=use renders (the rendered path and the materialized path agree at RUNTIME, not just
// by construction). Compiled ONLY under `-tags docker_integration`; it skips locally when dockerd is
// unreachable (the Windows worktree) and t.Fatals under $CI (no-skip-as-green).

package tools

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/moby/moby/client"
)

// dockerdReachableTools mirrors usersandbox.skipUnlessDockerd's stdlib socket probe (the package's
// own gate is _test-private). An npipe:// endpoint (Windows Docker Desktop) is not stdlib-dialable
// and is treated as unreachable — a LOCAL skip only, never a CI pass.
func skipUnlessDockerdTools(t *testing.T) {
	t.Helper()
	network, addr := "unix", "/var/run/docker.sock"
	if host := strings.TrimSpace(os.Getenv("DOCKER_HOST")); host != "" {
		switch {
		case strings.HasPrefix(host, "unix://"):
			network, addr = "unix", strings.TrimPrefix(host, "unix://")
		case strings.HasPrefix(host, "tcp://"):
			network, addr = "tcp", strings.TrimPrefix(host, "tcp://")
		default:
			network, addr = "", ""
		}
	}
	reachable := false
	if network != "" {
		if conn, err := net.DialTimeout(network, addr, 2*time.Second); err == nil {
			_ = conn.Close()
			reachable = true
		}
	}
	if reachable {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("docker_integration requires a reachable Docker daemon under CI (no-skip-as-green); wire dockerd into ci.yml")
	}
	t.Skip("docker_integration requires a reachable Docker daemon; start Docker and re-run")
}

func dockerTestImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE")); v != "" {
		return v
	}
	return "python:3-slim"
}

func newDockerRouter(t *testing.T, sources usersandbox.SourceResolver) *usersandbox.SandboxRouter {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	limits := usersandbox.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 1 << 30, PidsLimit: 256}
	opts := []usersandbox.Option{}
	if sources != nil {
		opts = append(opts, usersandbox.WithMaterializeSources(sources))
	}
	backend := usersandbox.NewDockerBackend(cli, dockerTestImage(), limits, opts...)
	return usersandbox.NewSandboxRouter(backend, config.ProfileServerProduction, config.SandboxConfig{
		Image: dockerTestImage(), CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256, IdleTTLSec: 1800,
	})
}

// TestRoute_StrictExecInBox proves a strict-profile shell_exec runs inside the box: a file written
// under /workspace is readable back IN the box, and the host cwd is never touched.
func TestRoute_StrictExecInBox(t *testing.T) {
	skipUnlessDockerdTools(t)
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
	skipUnlessDockerdTools(t)

	// Host-side snippet the box materializes at /skills/calc/calc.py.
	exportDir := t.TempDir()
	calcDir := filepath.Join(exportDir, "calc")
	if err := os.MkdirAll(calcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(calcDir, "calc.py"), []byte("print('SNIPPET-OK')\n"), 0o644); err != nil {
		t.Fatalf("write snippet: %v", err)
	}
	sources := func(string) []usersandbox.MaterializeSource {
		return []usersandbox.MaterializeSource{{HostDir: exportDir, Dest: "/skills"}}
	}
	router := newDockerRouter(t, sources)

	// skill action=use (strict) renders the SandboxPath the box materialized the snippet at.
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {instructions: "Add.", hostPath: filepath.Join(calcDir, "calc.py"), sandboxPath: "/skills/calc/calc.py", interpreter: "python3"},
	}
	skill := &SkillTool{Loader: loader, SandboxRouter: router}
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
