package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestCommandHook_BeforeToolRewrite(t *testing.T) {
	h := newTestCommandHook(t, "rewrite_tool", 2*time.Second, trustedHookHash(t))
	call := agenttest.MakeToolCall("c1", "echo", `{"v":"original"}`)

	res, err := h.BeforeTool(context.Background(), call)
	if err != nil {
		t.Fatalf("BeforeTool: %v", err)
	}
	if res == nil || res.Call == nil {
		t.Fatalf("BeforeTool result = %+v, want rewritten call", res)
	}
	if got := res.Call.Function.Arguments; got != `{"v":"command"}` {
		t.Fatalf("rewritten args = %q, want command", got)
	}
}

func TestCommandHook_BeforeToolDeny(t *testing.T) {
	h := newTestCommandHook(t, "deny_tool", 2*time.Second, trustedHookHash(t))
	call := agenttest.MakeToolCall("c1", "echo", `{"v":"real"}`)

	res, err := h.BeforeTool(context.Background(), call)
	if err != nil {
		t.Fatalf("BeforeTool: %v", err)
	}
	if res == nil || res.Result == nil {
		t.Fatalf("BeforeTool result = %+v, want veto result", res)
	}
	if got := res.Result.Preview; got != "denied by command" {
		t.Fatalf("preview = %q, want denied by command", got)
	}
}

func TestCommandHook_TrustHashMismatchRefusesBeforeExecution(t *testing.T) {
	badHash := strings.Repeat("0", 64)
	h := newTestCommandHook(t, "rewrite_tool", 2*time.Second, badHash)

	_, err := h.BeforeTool(context.Background(), agenttest.MakeToolCall("c1", "echo", `{}`))
	if err == nil || !strings.Contains(err.Error(), "trust hash mismatch") {
		t.Fatalf("BeforeTool error = %v, want trust hash mismatch", err)
	}
}

func TestCommandHook_TimeoutIsError(t *testing.T) {
	h := newTestCommandHook(t, "sleep", time.Millisecond, trustedHookHash(t))

	err := h.OnTurnStart(context.Background(), agent.HookTurn{AgentName: "aura"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("OnTurnStart error = %v, want timeout", err)
	}
}

func TestCommandHookHelperProcess(t *testing.T) {
	if os.Getenv("AURA_HOOK_HELPER") != "1" {
		return
	}
	mode := os.Getenv("AURA_HOOK_MODE")
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var event agent.CommandHookEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	switch mode {
	case "rewrite_tool":
		if event.Event != "before_tool" || event.ToolCall == nil {
			fmt.Fprintf(os.Stderr, "unexpected event: %+v\n", event)
			os.Exit(2)
		}
		call := *event.ToolCall
		call.Function.Arguments = `{"v":"command"}`
		writeHookDecision(agent.CommandHookDecision{Decision: "rewrite", ToolCall: &call})
	case "deny_tool":
		res := tools.ToolResult{Preview: "denied by command", Bytes: len("denied by command")}
		writeHookDecision(agent.CommandHookDecision{Decision: "deny", ToolResult: &res})
	case "sleep":
		time.Sleep(3 * time.Second)
		writeHookDecision(agent.CommandHookDecision{Decision: "allow"})
	default:
		fmt.Fprintf(os.Stderr, "unknown hook helper mode %q\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func newTestCommandHook(t *testing.T, mode string, timeout time.Duration, hash string) *agent.CommandHook {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	h, err := agent.NewCommandHook(agent.CommandHookConfig{
		Name:           "test-command-hook",
		Command:        exe,
		Args:           []string{"-test.run=TestCommandHookHelperProcess"},
		Env:            []string{"AURA_HOOK_HELPER=1", "AURA_HOOK_MODE=" + mode},
		ExpectedSHA256: hash,
		Timeout:        timeout,
	})
	if err != nil {
		t.Fatalf("NewCommandHook: %v", err)
	}
	return h
}

func trustedHookHash(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", exe, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeHookDecision(decision agent.CommandHookDecision) {
	if err := json.NewEncoder(os.Stdout).Encode(decision); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
