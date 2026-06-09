package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// TestPersistPause_ForwardsProxiedIDs pins the D-05 layer-3 plumb: persistPause
// (the SOLE paused_states writer) must read ProxiedFromChildID/ProxiedToolCallID
// off the *agent.AwaitingInput and forward them into askuser.InsertParams. The
// fixture uses the REAL flat worker id "w2" the swarm report carries and the model
// is told to relay — not a synthetic uuid that masked the CR-01 failure. A
// non-empty child id is passed as a non-nil *string; the tool_call id passes
// through verbatim.
func TestPersistPause_ForwardsProxiedIDs(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	ai := &agent.AwaitingInput{
		Question:           "relay?",
		Kind:               "clarification",
		ToolCallID:         "tc-parent",
		ProxiedFromChildID: "w2",
		ProxiedToolCallID:  "child-tc",
	}
	if err := r.persistPause(context.Background(), tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	got := pause.lastInsert
	if got.ProxiedFromChildID == nil || *got.ProxiedFromChildID != "w2" {
		t.Errorf("ProxiedFromChildID not forwarded: %v", got.ProxiedFromChildID)
	}
	if got.ProxiedToolCallID != "child-tc" {
		t.Errorf("ProxiedToolCallID not forwarded: got %q", got.ProxiedToolCallID)
	}
}

// TestPersistPause_DirectPauseLeavesProxiedNil is the back-compat control: a
// direct (non-proxied) pause forwards a nil child id (→ SQL NULL) and an empty
// tool_call id, so existing direct pauses persist unchanged.
func TestPersistPause_DirectPauseLeavesProxiedNil(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	ai := &agent.AwaitingInput{Question: "direct?", Kind: "approval", ToolCallID: "tc1"}
	if err := r.persistPause(context.Background(), tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	got := pause.lastInsert
	if got.ProxiedFromChildID != nil {
		t.Errorf("a direct pause must forward a nil child id (SQL NULL), got %v", *got.ProxiedFromChildID)
	}
	if got.ProxiedToolCallID != "" {
		t.Errorf("a direct pause must forward an empty tool_call id, got %q", got.ProxiedToolCallID)
	}
}

func TestPersistPause_ForwardsResumeContext(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	resumeContext := json.RawMessage(`{"type":"skill_approval","skill_name":"calc"}`)
	ai := &agent.AwaitingInput{
		Question:      "approve skill?",
		Kind:          "approval",
		ToolCallID:    "tc1",
		ResumeContext: resumeContext,
	}
	if err := r.persistPause(context.Background(), tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	if string(pause.lastInsert.ResumeContext) != string(resumeContext) {
		t.Fatalf("ResumeContext not forwarded: got %s want %s", pause.lastInsert.ResumeContext, resumeContext)
	}
}
