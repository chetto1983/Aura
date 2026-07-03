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
	"github.com/chetto1983/aura/internal/llm"
)

func TestCommandHook_BeforeModelRewrite(t *testing.T) {
	h := newTestCommandHook(t, "rewrite_model", 2*time.Second, trustedHookHash(t))
	req := llm.Request{Model: "original-model"}

	res, err := h.BeforeModel(context.Background(), &req)
	if err != nil {
		t.Fatalf("BeforeModel: %v", err)
	}
	if res != nil {
		t.Fatalf("BeforeModel result = %+v, want in-place rewrite", res)
	}
	if got := req.Model; got != "hook-model" {
		t.Fatalf("model = %q, want hook-model", got)
	}
}

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

func TestCommandHook_AfterToolRewrite(t *testing.T) {
	h := newTestCommandHook(t, "rewrite_result", 2*time.Second, trustedHookHash(t))
	call := agenttest.MakeToolCall("c1", "echo", `{"v":"real"}`)
	res := tools.ToolResult{Preview: "original", Bytes: len("original")}

	next, err := h.AfterTool(context.Background(), call, res)
	if err != nil {
		t.Fatalf("AfterTool: %v", err)
	}
	if next == nil || next.Result == nil {
		t.Fatalf("AfterTool result = %+v, want rewritten result", next)
	}
	if got := next.Result.Preview; got != "rewritten by command" {
		t.Fatalf("preview = %q, want rewritten by command", got)
	}
}

func TestCommandHook_OnTurnEndDeny(t *testing.T) {
	h := newTestCommandHook(t, "deny_turn_end", 2*time.Second, trustedHookHash(t))

	err := h.OnTurnEnd(context.Background(), agent.HookTurn{AgentName: "aura"})
	if err == nil || !strings.Contains(err.Error(), "stop end") {
		t.Fatalf("OnTurnEnd error = %v, want denial message", err)
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

func TestCommandHook_ChildEnvDropsSecretsByDefault(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-command-hook-secret")
	t.Setenv("AURA_DB_URL", "postgres://user:pass@localhost:5432/aura")
	t.Setenv("PUBLIC_URL", "https://example.com/app")

	h := newTestCommandHookWithEnv(t, "env_probe", 2*time.Second, trustedHookHash(t), []string{
		"EXPLICIT_PUBLIC=visible",
		"AURA_DB_URL=postgres://explicit:secret@localhost:5432/aura",
	})

	if err := h.OnTurnStart(context.Background(), agent.HookTurn{AgentName: "aura"}); err != nil {
		t.Fatalf("OnTurnStart: %v", err)
	}
}

func TestCommandHookManagerFromEnv_BuildsRunnableHook(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	raw := `[{
		"name":"env-command-hook",
		"command":` + quoteJSON(exe) + `,
		"args":["-test.run=TestCommandHookHelperProcess"],
		"env":["AURA_HOOK_HELPER=1","AURA_HOOK_MODE=rewrite_model"],
		"expected_sha256":` + quoteJSON(trustedHookHash(t)) + `,
		"timeout_ms":2000
	}]`

	m, err := agent.CommandHookManagerFromEnv(func(key string) (string, bool) {
		if key == agent.EnvCommandHooks {
			return raw, true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("CommandHookManagerFromEnv: %v", err)
	}
	if m == nil {
		t.Fatal("CommandHookManagerFromEnv returned nil manager for configured hooks")
	}

	req := &llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "before"}}}
	res, err := m.BeforeModel(context.Background(), req)
	if err != nil {
		t.Fatalf("BeforeModel through env hook: %v", err)
	}
	if res != nil {
		t.Fatalf("rewrite_model should mutate the request in place, got result %+v", res)
	}
	if got := req.Model; got != "hook-model" {
		t.Fatalf("request model = %q, want env command hook rewrite", got)
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
	case "rewrite_model":
		if event.Event != "before_model" || event.Request == nil {
			fmt.Fprintf(os.Stderr, "unexpected event: %+v\n", event)
			os.Exit(2)
		}
		req := *event.Request
		req.Model = "hook-model"
		writeHookDecision(agent.CommandHookDecision{Decision: "rewrite", Request: &req})
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
	case "rewrite_result":
		res := tools.ToolResult{Preview: "rewritten by command", Bytes: len("rewritten by command")}
		writeHookDecision(agent.CommandHookDecision{Decision: "rewrite", ToolResult: &res})
	case "deny_turn_end":
		writeHookDecision(agent.CommandHookDecision{Decision: "deny", Message: "stop end"})
	case "rewrite_model_then_crash":
		req := *event.Request
		req.Model = "hook-model"
		writeHookDecision(agent.CommandHookDecision{Decision: "rewrite", Request: &req})
		os.Exit(7)
	case "deny_then_crash":
		writeHookDecision(agent.CommandHookDecision{Decision: "deny", Message: "crashed but denied"})
		os.Exit(7)
	case "allow_then_crash":
		// A hook that tries to ALLOW then exits non-zero must NOT silently allow:
		// run() ignores a crashed allow and returns an error (GATE-02 fail-closed).
		writeHookDecision(agent.CommandHookDecision{Decision: "allow"})
		os.Exit(7)
	case "crash_no_decision":
		// Non-zero exit with unparseable stdout (no allow/deny decision) → deny.
		fmt.Fprint(os.Stdout, "this is not a valid decision")
		os.Exit(7)
	case "oversized_model_rewrite":
		req := *event.Request
		req.Messages = make([]llm.Message, 0, 5000)
		for i := 0; i < 5000; i++ {
			req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Content: "x"})
		}
		writeHookDecision(agent.CommandHookDecision{Decision: "rewrite", Request: &req})
	case "sleep":
		time.Sleep(3 * time.Second)
		writeHookDecision(agent.CommandHookDecision{Decision: "allow"})
	case "env_probe":
		for _, key := range []string{"OPENROUTER_API_KEY", "AURA_DB_URL"} {
			if os.Getenv(key) != "" {
				fmt.Fprintf(os.Stderr, "secret env %s leaked\n", key)
				os.Exit(3)
			}
		}
		if os.Getenv("EXPLICIT_PUBLIC") != "visible" {
			fmt.Fprintln(os.Stderr, "explicit public env missing")
			os.Exit(3)
		}
		writeHookDecision(agent.CommandHookDecision{Decision: "allow"})
	default:
		fmt.Fprintf(os.Stderr, "unknown hook helper mode %q\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func newTestCommandHook(t *testing.T, mode string, timeout time.Duration, hash string) *agent.CommandHook {
	t.Helper()
	return newTestCommandHookWithEnv(t, mode, timeout, hash, nil)
}

func newTestCommandHookWithEnv(t *testing.T, mode string, timeout time.Duration, hash string, extraEnv []string) *agent.CommandHook {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	env := []string{"AURA_HOOK_HELPER=1", "AURA_HOOK_MODE=" + mode}
	env = append(env, extraEnv...)
	h, err := agent.NewCommandHook(agent.CommandHookConfig{
		Name:           "test-command-hook",
		Command:        exe,
		Args:           []string{"-test.run=TestCommandHookHelperProcess"},
		Env:            env,
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

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func writeHookDecision(decision agent.CommandHookDecision) {
	if err := json.NewEncoder(os.Stdout).Encode(decision); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
