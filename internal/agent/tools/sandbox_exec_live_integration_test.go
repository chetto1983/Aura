//go:build sandbox_integration

// Live tool-layer E2E for sandbox_exec against the real aura-sandbox-agent
// container. Where internal/sandboxagent's live test exercises the raw HTTP
// client, this drives Aura's actual tool object — the exact SandboxExec.Execute
// the agent's tool dispatcher invokes — with the raw JSON shape the LLM emits.
// It covers node and npm, the Node toolchain the client-layer test does not.
//
// Requires the container healthy (`make sandbox-up`). Run:
//
//	AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 \
//	  go test -tags sandbox_integration -run TestSandboxExecLive ./internal/agent/tools/ -v
package tools

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

// liveSandboxRunnerOrSkip builds a real sandbox-agent client against the live
// container. When AURA_SANDBOX_AGENT_URL is unset it skips locally and t.Fatals
// under $CI (no-skip-as-green, mirrors internal/sandboxagent.sandboxOrSkip).
//
// The client uses http.DefaultTransport, whose keep-alive readLoop/writeLoop
// goroutines outlive the request; the tools package runs goleak in TestMain, so
// cleanup closes idle connections to let them exit before verification.
func liveSandboxRunnerOrSkip(t *testing.T) *sandboxagent.Client {
	t.Helper()
	base := os.Getenv("AURA_SANDBOX_AGENT_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("sandbox_integration requires AURA_SANDBOX_AGENT_URL, but it is unset under CI — " +
				"a skipped integration test must not pass as green; wire it in ci.yml")
		}
		t.Skip("sandbox_integration requires AURA_SANDBOX_AGENT_URL; run `make sandbox-up` (default http://127.0.0.1:2468)")
	}
	t.Cleanup(func() {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	})
	return sandboxagent.New(sandboxagent.Config{BaseURL: base, TimeoutSec: 30})
}

// TestSandboxExecLive_Node proves Aura's sandbox_exec tool runs node in the live
// container end-to-end: the LLM-shaped JSON args flow through Execute, the real
// client hits /v1/processes/run, and the ToolResult preview carries exit_code 0
// plus node's stdout.
func TestSandboxExecLive_Node(t *testing.T) {
	tool := &SandboxExec{Runner: liveSandboxRunnerOrSkip(t)}
	ctx := ctxWith(t, "sess-live-node", "call-live-node")

	res, err := tool.Execute(ctx, json.RawMessage(
		`{"command":"node","args":["-e","process.stdout.write('node-e2e-ok ' + process.version)"]}`))
	if err != nil {
		t.Fatalf("Execute node: %v", err)
	}
	if !strings.Contains(res.Preview, "node-e2e-ok v") {
		t.Fatalf("preview missing node stdout: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, `"exit_code":0`) {
		t.Fatalf("preview missing exit_code 0: %q", res.Preview)
	}
}

// TestSandboxExecLive_Npm proves npm is callable through the same tool path.
func TestSandboxExecLive_Npm(t *testing.T) {
	tool := &SandboxExec{Runner: liveSandboxRunnerOrSkip(t)}
	ctx := ctxWith(t, "sess-live-npm", "call-live-npm")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"npm","args":["--version"]}`))
	if err != nil {
		t.Fatalf("Execute npm: %v", err)
	}
	if !strings.Contains(res.Preview, `"exit_code":0`) {
		t.Fatalf("npm --version exit_code != 0: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, `"stdout":"`) || strings.Contains(res.Preview, `"stdout":""`) {
		t.Fatalf("npm --version produced no stdout: %q", res.Preview)
	}
}
