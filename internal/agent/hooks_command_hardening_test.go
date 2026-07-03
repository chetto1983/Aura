package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// AG-030: a hook that emits a rewrite then exits non-zero must NOT have its
// rewrite applied as success — a crashed-after-emitting rewrite is rejected.
func TestCommandHook_NonZeroExitRewriteRejected(t *testing.T) {
	h := newTestCommandHook(t, "rewrite_model_then_crash", 2*time.Second, trustedHookHash(t))
	req := llm.Request{Model: "original-model"}

	_, err := h.BeforeModel(context.Background(), &req)
	if err == nil {
		t.Fatal("BeforeModel with non-zero-exit rewrite err = nil, want rejection")
	}
	if req.Model == "hook-model" {
		t.Fatal("non-zero-exit rewrite was applied; want original request preserved")
	}
}

// AG-030: a deny decision remains honored even on a non-zero exit (denial is
// security-safe — it does not apply attacker-influenced state).
func TestCommandHook_NonZeroExitDenyHonored(t *testing.T) {
	h := newTestCommandHook(t, "deny_then_crash", 2*time.Second, trustedHookHash(t))
	call := agenttest.MakeToolCall("c1", "echo", `{}`)

	res, err := h.BeforeTool(context.Background(), call)
	if err != nil {
		t.Fatalf("BeforeTool deny-on-crash err = %v, want honored", err)
	}
	if res == nil || res.Result == nil {
		t.Fatalf("BeforeTool deny-on-crash res = %+v, want veto result", res)
	}
}

// AG-003 rewrite bounds: an oversized hook rewrite (too many messages) is
// rejected before replacing live request state.
func TestCommandHook_OversizedRewriteRejected(t *testing.T) {
	h := newTestCommandHook(t, "oversized_model_rewrite", 2*time.Second, trustedHookHash(t))
	req := llm.Request{Model: "original-model"}

	_, err := h.BeforeModel(context.Background(), &req)
	if err == nil {
		t.Fatal("BeforeModel oversized rewrite err = nil, want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rewrite") {
		t.Fatalf("oversized rewrite err = %v, want rewrite-bounds reason", err)
	}
	if req.Model == "hook-model" {
		t.Fatal("oversized rewrite was applied; want original request preserved")
	}
}

// AG-054: bare hook command names (resolved via runtime PATH) are rejected;
// only absolute paths are accepted.
func TestNewCommandHook_RejectsBareCommandName(t *testing.T) {
	_, err := agent.NewCommandHook(agent.CommandHookConfig{
		Name:           "bare",
		Command:        "go",
		ExpectedSHA256: strings.Repeat("a", 64),
	})
	if err == nil {
		t.Fatal("NewCommandHook with bare name err = nil, want absolute-path rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "absolute") {
		t.Fatalf("bare-name err = %v, want absolute-path reason", err)
	}
}

// --- GATE-02: a faulting command hook DENIES through the HookManager ----------
//
// The tests above exercise a CommandHook in isolation. The ones below wire it
// into a HookManager exactly as CommandHookManagerFromEnv does and drive the
// real dispatch tool-gate, proving that timeout / crashed-allow / unparseable
// crash all abort the turn (hookFault under the default FailClosed policy) and
// that the gated tool's Execute is NEVER reached — a denied command cannot be
// silently allowed. The fail_open contrast lives in hooks_policy_test.go.

// spyTool stands in for a gated tool: Execute flips executed so a fault-denied
// turn can prove the tool never ran.
type spyTool struct{ executed *bool }

func (spyTool) Spec() tools.Spec {
	return tools.Spec{Name: "echo", Summary: "gate-02 spy tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (s spyTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	*s.executed = true
	return tools.ToolResult{Preview: "spy executed", Bytes: len("spy executed")}, nil
}

// gateTool mirrors the dispatch tool-gate (llm_agent_dispatch.go:63-75): the
// manager's BeforeTool is consulted BEFORE the tool executes. A non-nil error
// aborts the turn (dispatch returns `false, err`) so executeBatch — and thus
// tool.Execute — is never reached. A veto Result (nil err) also skips Execute.
// It returns the gate error so a test asserts deny + tool-not-run in one place.
func gateTool(m *agent.HookManager, call llm.ToolCall, tool tools.Tool) error {
	res, err := m.BeforeTool(context.Background(), call)
	if err != nil {
		return err
	}
	if res != nil && res.Result != nil {
		return nil
	}
	_, execErr := tool.Execute(context.Background(), json.RawMessage(call.Function.Arguments))
	return execErr
}

// newCommandHookManager builds a HookManager the operator way — through
// CommandHookManagerFromEnv, honoring the per-hook fail_policy. An empty
// failPolicy exercises the DEFAULT (FailClosed) resolution.
func newCommandHookManager(t *testing.T, mode, failPolicy string, timeoutMS int) *agent.HookManager {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	raw, err := json.Marshal([]agent.CommandHookEnvConfig{{
		Name:           "gate-02-hook",
		Command:        exe,
		Args:           []string{"-test.run=TestCommandHookHelperProcess"},
		Env:            []string{"AURA_HOOK_HELPER=1", "AURA_HOOK_MODE=" + mode},
		ExpectedSHA256: trustedHookHash(t),
		TimeoutMS:      timeoutMS,
		FailPolicy:     failPolicy,
	}})
	if err != nil {
		t.Fatalf("marshal hook config: %v", err)
	}
	m, err := agent.CommandHookManagerFromEnv(func(key string) (string, bool) {
		if key == agent.EnvCommandHooks {
			return string(raw), true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("CommandHookManagerFromEnv: %v", err)
	}
	if m == nil {
		t.Fatal("CommandHookManagerFromEnv returned nil manager for a configured hook")
	}
	return m
}

func TestCommandHook_TimeoutDeniesTurnToolNeverExecutes(t *testing.T) {
	m := newCommandHookManager(t, "sleep", "", 1) // unset fail_policy → default FailClosed
	executed := false

	err := gateTool(m, agenttest.MakeToolCall("c1", "echo", `{}`), spyTool{executed: &executed})
	if err == nil {
		t.Fatal("timeout under default policy: gate err = nil, want DENY (turn abort)")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("deny err = %v, want timeout reason", err)
	}
	if executed {
		t.Fatal("gated tool executed despite hook timeout; want never reached")
	}
}

func TestCommandHook_CrashAllowDeniesTurnToolNeverExecutes(t *testing.T) {
	m := newCommandHookManager(t, "allow_then_crash", "", 2000)
	executed := false

	err := gateTool(m, agenttest.MakeToolCall("c1", "echo", `{}`), spyTool{executed: &executed})
	if err == nil {
		t.Fatal("crashed-allow under default policy: gate err = nil, want DENY (cannot silently allow)")
	}
	if executed {
		t.Fatal("gated tool executed after a crashed allow; a denied command was silently allowed")
	}
}

func TestCommandHook_CrashNoDecisionDeniesTurnToolNeverExecutes(t *testing.T) {
	m := newCommandHookManager(t, "crash_no_decision", "", 2000)
	executed := false

	err := gateTool(m, agenttest.MakeToolCall("c1", "echo", `{}`), spyTool{executed: &executed})
	if err == nil {
		t.Fatal("crash with no parseable decision: gate err = nil, want DENY")
	}
	if executed {
		t.Fatal("gated tool executed after a crash with no parseable decision; want never reached")
	}
}

// TestCommandHook_CrashRewriteDeniedViaManager proves AG-030 end-to-end: a hook
// that emits a before_model rewrite then exits non-zero is rejected THROUGH the
// HookManager (hookFault/FailClosed), leaving the live request unchanged.
func TestCommandHook_CrashRewriteDeniedViaManager(t *testing.T) {
	m := newCommandHookManager(t, "rewrite_model_then_crash", "", 2000)
	req := llm.Request{Model: "original-model"}

	if _, err := m.BeforeModel(context.Background(), &req); err == nil {
		t.Fatal("crashed rewrite via manager: err = nil, want AG-030 rejection")
	}
	if req.Model == "hook-model" {
		t.Fatal("crashed rewrite was applied through the manager; want original request preserved")
	}
}
