package swarm

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/chat"
	silentadapter "github.com/aura/aura/internal/channels/silent"
	"github.com/aura/aura/internal/identity"
)

// ── NodeSpec validation ──────────────────────────────────────────────────────

func TestNodeSpecValidateEmptyGoal(t *testing.T) {
	spec := NodeSpec{RiskTier: "read_only"}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for empty Goal, got nil")
	}
}

func TestNodeSpecValidateWriteProposalTier(t *testing.T) {
	spec := NodeSpec{Goal: "do something", RiskTier: "write_proposal"}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for write_proposal RiskTier, got nil")
	}
}

func TestNodeSpecValidateBudgetCap(t *testing.T) {
	spec := NodeSpec{Goal: "do something", RiskTier: "read_only", BudgetSecs: 301}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for BudgetSecs > 300, got nil")
	}
}

// ── HubBridge.Dispatch ───────────────────────────────────────────────────────

// newDispatchHub builds a minimal Hub + silent outbound adapter for Dispatch
// tests. Uses captureLoop from hub_e2e_test.go (same package).
func newDispatchHub(t *testing.T, loop *captureLoop) *chat.Hub {
	t.Helper()
	hub, err := chat.New(chat.Config{Loop: loop})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	hub.RegisterOutbound(silentadapter.NewSwarm(silentadapter.Config{}))
	return hub
}

func TestHubBridgeDispatchShape(t *testing.T) {
	loop := &captureLoop{}
	hub := newDispatchHub(t, loop)
	bridge := NewHubBridge(hub, "")

	spec := NodeSpec{
		Goal:          "search the web for Go concurrency patterns",
		Instruction:   "be concise",
		ToolAllowlist: []string{"web_search"},
		MaxIterations: 3,
		MaxToolCalls:  5,
		BudgetSecs:    60,
		RiskTier:      "read_only",
		ParentRunID:   "parent-run-111",
		AssignmentID:  "assign-abc",
	}
	principal := identity.Principal{ID: "user-1"}

	runID, err := bridge.Dispatch(context.Background(), spec, principal)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if runID == "" {
		t.Fatal("Dispatch returned empty runID")
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()

	if len(loop.msgs) != 1 {
		t.Fatalf("expected 1 captured message, got %d", len(loop.msgs))
	}
	msg := loop.msgs[0]

	if msg.Channel != chat.ChannelSwarm {
		t.Errorf("Channel = %q, want %q", msg.Channel, chat.ChannelSwarm)
	}
	if msg.Mode != chat.DeliveryModeSilent {
		t.Errorf("Mode = %q, want %q", msg.Mode, chat.DeliveryModeSilent)
	}
	if msg.Text != spec.Goal {
		t.Errorf("Text = %q, want %q", msg.Text, spec.Goal)
	}

	for _, key := range []string{"parent_run_id", "assignment_id", "instruction", "tool_allowlist", "max_iterations", "max_tool_calls", "budget_secs"} {
		if _, ok := msg.ChannelData[key]; !ok {
			t.Errorf("ChannelData missing key %q", key)
		}
	}
}

func TestHubBridgeDispatchParentRunIDPropagation(t *testing.T) {
	loop := &captureLoop{}
	hub := newDispatchHub(t, loop)
	bridge := NewHubBridge(hub, "")

	spec := NodeSpec{
		Goal:        "summarize recent events",
		RiskTier:    "read_only",
		ParentRunID: "parent-run-xyz",
	}

	_, err := bridge.Dispatch(context.Background(), spec, identity.Principal{ID: "u1"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()

	if len(loop.msgs) == 0 {
		t.Fatal("no messages captured")
	}
	msg := loop.msgs[0]

	if msg.ParentRunID != "parent-run-xyz" {
		t.Errorf("InboundMessage.ParentRunID = %q, want parent-run-xyz", msg.ParentRunID)
	}
	if v, _ := msg.ChannelData["parent_run_id"].(string); v != "parent-run-xyz" {
		t.Errorf("ChannelData[parent_run_id] = %q, want parent-run-xyz", v)
	}
}
