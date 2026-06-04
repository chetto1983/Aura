//go:build sandbox_integration

// Live integration tests for internal/sandboxagent against the real
// aura-sandbox-agent container. Requires the container to be healthy:
//
//	make sandbox-up
//
// Run:
//
//	go test -tags sandbox_integration -run TestSandboxAgentLive ./internal/sandboxagent/ -v
//	go test -tags sandbox_integration -run TestSandboxAgentLive ./internal/sandboxagent/ -v -race
//
// No-skip-as-green: sandboxOrSkip t.Fatals under $CI when AURA_SANDBOX_AGENT_URL
// is unset, so a misconfigured CI job never passes as green.
package sandboxagent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// sandboxOrSkip returns the sandbox agent base URL. When AURA_SANDBOX_AGENT_URL
// is unset it skips locally and t.Fatals under $CI (no-skip-as-green rule,
// mirrors internal/db.envOrSkip).
func sandboxOrSkip(t *testing.T) string {
	t.Helper()
	v := os.Getenv("AURA_SANDBOX_AGENT_URL")
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("sandbox_integration requires AURA_SANDBOX_AGENT_URL, but it is unset under CI — " +
				"a skipped integration test must not pass as green; wire it in ci.yml")
		}
		t.Skip("sandbox_integration requires AURA_SANDBOX_AGENT_URL; run `make sandbox-up` and set the var (or use the default http://127.0.0.1:2468)")
	}
	return v
}

// TestSandboxAgentLive_PythonEval verifies that the live container executes
// python -c "print(40+2)" and returns stdout="42\n", exit_code=0, timed_out=false.
func TestSandboxAgentLive_PythonEval(t *testing.T) {
	baseURL := sandboxOrSkip(t)

	c := New(Config{BaseURL: baseURL, TimeoutSec: 30})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := c.Run(ctx, RunRequest{
		Command: "python",
		Args:    []string{"-c", "print(40+2)"},
	})
	if err != nil {
		t.Fatalf("Run python eval: %v", err)
	}

	if !strings.Contains(res.Stdout, "42") {
		t.Fatalf("stdout = %q, want content containing \"42\"", res.Stdout)
	}
	if res.ExitCode == nil {
		t.Fatal("exit_code is nil, want 0")
	}
	if *res.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0 (stderr: %q)", *res.ExitCode, res.Stderr)
	}
	if res.TimedOut {
		t.Fatal("timed_out = true, want false")
	}
}

// TestSandboxAgentLive_WorkspacePersistence verifies that the /workspace named
// volume persists written data across separate Client.Run invocations. A unique
// filename prevents cross-run collisions.
func TestSandboxAgentLive_WorkspacePersistence(t *testing.T) {
	baseURL := sandboxOrSkip(t)

	c := New(Config{BaseURL: baseURL, TimeoutSec: 30})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Unique filename: test name + PID avoids collision with parallel runs.
	filename := fmt.Sprintf("/workspace/nyquist_persist_%d.txt", os.Getpid())
	content := fmt.Sprintf("aura-sandbox-persist-%d", os.Getpid())

	// Cleanup the test file regardless of outcome.
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_, _ = c.Run(cleanCtx, RunRequest{
			Command: "rm",
			Args:    []string{"-f", filename},
		})
	})

	// Call 1: write the unique file under /workspace.
	writeRes, err := c.Run(ctx, RunRequest{
		Command: "python",
		Args:    []string{"-c", fmt.Sprintf("open(%q, 'w').write(%q)", filename, content)},
	})
	if err != nil {
		t.Fatalf("write call: %v", err)
	}
	if writeRes.ExitCode == nil || *writeRes.ExitCode != 0 {
		ec := -1
		if writeRes.ExitCode != nil {
			ec = *writeRes.ExitCode
		}
		t.Fatalf("write call exit_code=%d, stderr=%q", ec, writeRes.Stderr)
	}

	// Call 2 (separate invocation): read the file back — proves named-volume
	// persistence across independent process runs.
	readRes, err := c.Run(ctx, RunRequest{
		Command: "python",
		Args:    []string{"-c", fmt.Sprintf("print(open(%q).read(), end='')", filename)},
	})
	if err != nil {
		t.Fatalf("read call: %v", err)
	}
	if readRes.ExitCode == nil || *readRes.ExitCode != 0 {
		ec := -1
		if readRes.ExitCode != nil {
			ec = *readRes.ExitCode
		}
		t.Fatalf("read call exit_code=%d, stderr=%q", ec, readRes.Stderr)
	}
	if readRes.Stdout != content {
		t.Fatalf("read stdout = %q, want %q (persistence failed)", readRes.Stdout, content)
	}
}
