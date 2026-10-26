package runner

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/approvaltext"
)

// TestPersistPause_ForwardsProxiedIDs pins the D-05 layer-3 plumb: persistPause must read
// ProxiedFromChildID/ProxiedToolCallID off the *agent.AwaitingInput and forward them into
// the ACCUMULATED askuser.InsertParams (post-34-06 persistPause no longer inserts — the
// row is written by flushPause's CommitPause; here we assert the tracker payload). The
// fixture uses the REAL flat worker id "w2" the swarm report carries and the model is told
// to relay — not a synthetic uuid that masked the CR-01 failure. A non-empty child id is
// passed as a non-nil *string; the tool_call id passes through verbatim.
func TestPersistPause_ForwardsProxiedIDs(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	ai := &agent.AwaitingInput{
		Question:           "relay?",
		Kind:               "clarification",
		ToolCallID:         "tc-parent",
		ProxiedFromChildID: "w2",
		ProxiedToolCallID:  "child-tc",
	}
	if err := r.persistPause(tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	if len(tr.pauseInserts) != 1 {
		t.Fatalf("persistPause must accumulate exactly 1 pause InsertParams, got %d", len(tr.pauseInserts))
	}
	got := tr.pauseInserts[0]
	if got.ProxiedFromChildID == nil || *got.ProxiedFromChildID != "w2" {
		t.Errorf("ProxiedFromChildID not forwarded: %v", got.ProxiedFromChildID)
	}
	if got.ProxiedToolCallID != "child-tc" {
		t.Errorf("ProxiedToolCallID not forwarded: got %q", got.ProxiedToolCallID)
	}
}

// TestPersistPause_DirectPauseLeavesProxiedNil is the direct-pause control: a
// direct (non-proxied) pause forwards a nil child id (→ SQL NULL) and an empty
// tool_call id, so existing direct pauses persist unchanged.
func TestPersistPause_DirectPauseLeavesProxiedNil(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	ai := &agent.AwaitingInput{Question: "direct?", Kind: "approval", ToolCallID: "tc1"}
	if err := r.persistPause(tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	if len(tr.pauseInserts) != 1 {
		t.Fatalf("persistPause must accumulate exactly 1 pause InsertParams, got %d", len(tr.pauseInserts))
	}
	got := tr.pauseInserts[0]
	if got.ProxiedFromChildID != nil {
		t.Errorf("a direct pause must forward a nil child id (SQL NULL), got %v", *got.ProxiedFromChildID)
	}
	if got.ProxiedToolCallID != "" {
		t.Errorf("a direct pause must forward an empty tool_call id, got %q", got.ProxiedToolCallID)
	}
}

func TestPersistPause_ForwardsResumeContext(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}

	resumeContext := json.RawMessage(
		`{"type":"skill_approval","skill_name":"calc","allowed_decisions":["decline"]}`,
	)
	ai := &agent.AwaitingInput{
		Question:      "approve skill?",
		Kind:          "approval",
		ToolCallID:    "tc1",
		ResumeContext: resumeContext,
	}
	if err := r.persistPause(tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}

	if len(tr.pauseInserts) != 1 {
		t.Fatalf("persistPause must accumulate exactly 1 pause InsertParams, got %d", len(tr.pauseInserts))
	}
	var got struct {
		Type             string   `json:"type"`
		SkillName        string   `json:"skill_name"`
		AllowedDecisions []string `json:"allowed_decisions"`
	}
	if err := json.Unmarshal(tr.pauseInserts[0].ResumeContext, &got); err != nil {
		t.Fatalf("ResumeContext invalid: %v", err)
	}
	if got.Type != "skill_approval" || got.SkillName != "calc" {
		t.Fatalf("ResumeContext fields not preserved: %s", tr.pauseInserts[0].ResumeContext)
	}
	if !slices.Equal(got.AllowedDecisions, allResumeDecisions()) {
		t.Fatalf("allowed decisions = %v, want %v", got.AllowedDecisions, allResumeDecisions())
	}
}

func TestPersistPauseReplacesRelayedApprovalPresentation(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	tr := &turnTracker{convID: newConvID(t)}
	question := "Approve files.delete (risk=high)?\nThis mutating action is WITHHELD until you accept.\nargs: path"
	ai := &agent.AwaitingInput{
		Question:   question,
		Kind:       "approval",
		ToolCallID: "tc1",
		ResumeContext: json.RawMessage(`{
			"type":"gateway_approval","tool":"files.delete","tier":"high",
			"approval_presentation":{"key":"approval.shell.command","params":{"cwd":"/","command":"rm -rf /","digest":"fake"}}
		}`),
	}
	if err := r.persistPause(tr, ai); err != nil {
		t.Fatalf("persistPause: %v", err)
	}
	presentation, ok := approvaltext.FromContext(question, tr.pauseInserts[0].ResumeContext)
	if !ok || presentation.Key != approvaltext.GatewayKey {
		t.Fatalf("trusted presentation = %+v, %v; context=%s", presentation, ok, tr.pauseInserts[0].ResumeContext)
	}
	if presentation.Params["args"] != "path" || presentation.Params["command"] != "" {
		t.Fatalf("relayed presentation was not replaced: %+v", presentation)
	}
}

// The runner stamps a relayed gateway_approval pause from the gateway's challenge (spec
// 2026-10-12): the target the operator reads and the withheld call's tool_call_id come from
// the server, whatever the model copied there. A lookup that finds nothing under this
// conversation removes both, so a target the model wrote never reaches the cockpit or Telegram.
// The model's copies go under any spelling: a struct decode would read "Target" as target.
func TestPersistPauseStampsTheGatewayChallenge(t *testing.T) {
	const question = "Approve board (risk=destructive)?\nThis mutating action is WITHHELD until you accept.\nargs: action, id"
	relayed := json.RawMessage(`{"type":"gateway_approval","tool":"board","tier":"destructive","args_sha256":"fp-1",` +
		`"target":"written by the model","tool_call_id":"call-forged","Target":"by case","TOOL_CALL_ID":"call-case"}`)
	stampedConv := newConvID(t)
	lookup := func(target string) ApprovalChallengeLookup {
		return func(conversationID, tool, argsFingerprint string) (string, string, bool) {
			if conversationID == stampedConv && tool == "board" && argsFingerprint == "fp-1" {
				return target, "call-withheld", true
			}
			return "", "", false
		}
	}
	tests := []struct {
		name   string
		convID string
		lookup ApprovalChallengeLookup
		want   map[string]string // the stamped root fields; a field not listed must be gone
	}{
		{"the challenge replaces what the model copied", stampedConv, lookup("Fattura fornitore · Fatto"),
			map[string]string{"target": "Fattura fornitore · Fatto", "tool_call_id": "call-withheld"}},
		{"an undescribed call gets its id and no target", stampedConv, lookup(""),
			map[string]string{"tool_call_id": "call-withheld"}},
		{"another conversation's challenge is a miss", newConvID(t), lookup("Fattura fornitore · Fatto"), map[string]string{}},
		{"no lookup wired is a miss", stampedConv, nil, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
			r.approvalChallenges = tt.lookup
			tr := &turnTracker{convID: tt.convID}
			ai := &agent.AwaitingInput{Question: question, Kind: "approval", ToolCallID: "call-ask", ResumeContext: relayed}
			if err := r.persistPause(tr, ai); err != nil {
				t.Fatalf("persistPause: %v", err)
			}
			saved := tr.pauseInserts[0].ResumeContext
			assertRootFields(t, saved, tt.want)
			p, ok := approvaltext.FromContext(question, saved)
			if !ok || p.Params["target"] != tt.want["target"] {
				t.Fatalf("presentation = %+v (%v), want target %q", p, ok, tt.want["target"])
			}
		})
	}
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.approvalChallenges = lookup("Fattura fornitore · Fatto")
	tr := &turnTracker{convID: stampedConv}
	other := json.RawMessage(`{"type":"shell_exec_approval","command_sha256":"d","target":"t","tool_call_id":"c"}`)
	if err := r.persistPause(tr, &agent.AwaitingInput{Question: "q", Kind: "approval", ResumeContext: other}); err != nil {
		t.Fatalf("persistPause: %v", err)
	}
	assertRootFields(t, tr.pauseInserts[0].ResumeContext, map[string]string{"target": "t", "tool_call_id": "c"})
	if _, err := stampGatewayChallenge(json.RawMessage(`[]`), stampedConv, nil); err == nil {
		t.Fatal("a resume context that is not an object was stamped")
	}
}

// assertRootFields checks the target and tool_call_id root fields of a saved resume context.
func assertRootFields(t *testing.T, saved json.RawMessage, want map[string]string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(saved, &fields); err != nil {
		t.Fatalf("resume context: %v", err)
	}
	for _, name := range []string{"target", "tool_call_id"} {
		raw, present := fields[name]
		value, wanted := want[name]
		var got string
		if present && json.Unmarshal(raw, &got) != nil {
			t.Fatalf("%s = %s, want a string", name, raw)
		}
		if present != wanted || got != value {
			t.Errorf("%s = %q (present %v), want %q (present %v); context %s", name, got, present, value, wanted, saved)
		}
		for key := range fields {
			if key != name && strings.EqualFold(key, name) {
				t.Errorf("the model's %q survived the stamp; context %s", key, saved)
			}
		}
	}
}

// TestAnyInt_JSONNumber pins M-07: a token-count value arriving as a json.Number
// (the StateDelta path decodes with UseNumber, and a jsonb round-trip widens
// numerics the same way) must be parsed to its int value, not silently zeroed. A
// non-numeric json.Number falls back to 0 like any other unparseable input.
func TestAnyInt_JSONNumber(t *testing.T) {
	if got := anyInt(json.Number("42")); got != 42 {
		t.Errorf("anyInt(json.Number(\"42\")) = %d, want 42", got)
	}
	if got := anyInt(json.Number("nope")); got != 0 {
		t.Errorf("anyInt(json.Number(\"nope\")) = %d, want 0 fallback", got)
	}
	// Existing cases stay intact.
	if got := anyInt(int(7)); got != 7 {
		t.Errorf("anyInt(int(7)) = %d, want 7", got)
	}
	if got := anyInt("string"); got != 0 {
		t.Errorf("anyInt(string) = %d, want 0", got)
	}
}

// The turn's usage carries two different numbers and the persisted row needs both: the
// bill (prompt_tokens, summed over the round's calls because each is charged and each
// re-sends the prefix) and the fill (context_tokens, the prompt of the FINAL call, which
// is what the window actually held). Reconstructing only the bill made the stored fill 0
// on every turn. Measured on a real tool round: 54,688 billed against ~23,000 held.
func TestUsageFromStateDeltaCarriesTheFillBesideTheBill(t *testing.T) {
	u := usageFromStateDelta(map[string]any{
		"prompt_tokens":     54688,
		"context_tokens":    23011,
		"completion_tokens": 643,
		"cache_hit_tokens":  0,
	})
	if u.PromptTokens != 54688 {
		t.Errorf("PromptTokens = %d, want the bill", u.PromptTokens)
	}
	if u.ContextTokens != 23011 {
		t.Errorf("ContextTokens = %d, want the fill", u.ContextTokens)
	}
	// A delta from an older daemon has no context_tokens; 0 is the honest answer, and the
	// read falls back to input_tokens for those rows rather than reporting an empty window.
	if got := usageFromStateDelta(map[string]any{"prompt_tokens": 100}); got.ContextTokens != 0 {
		t.Errorf("ContextTokens = %d on a delta without one, want 0", got.ContextTokens)
	}
}
