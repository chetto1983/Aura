package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
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
