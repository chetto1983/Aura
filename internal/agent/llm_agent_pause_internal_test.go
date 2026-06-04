package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// fakePauseTool returns the ErrAwaitingUserInput sentinel from Execute under a
// caller-chosen name. It lets a white-box test prove pauseCalls/detectPause gate
// on the ask_user NAME, not merely on "did Execute return the pause sentinel".
type fakePauseTool struct {
	name string
}

func (f fakePauseTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        f.name,
		Summary:     "stub",
		Description: "stub",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (fakePauseTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, &tools.ErrAwaitingUserInput{Question: "fake", Kind: tools.KindApproval}
}

func newBareAgent(t *testing.T, r *tools.Registry) *LlmAgent {
	t.Helper()
	return NewLlmAgent(LlmAgentConfig{
		LLM:       llm.Config{Model: "m", Provider: "test", TotalTimeoutSec: 30},
		Registry:  r,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
	})
}

func internalPauseIC(t *testing.T) InvocationContext {
	t.Helper()
	return InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7())}
}

func toolCall(id, name, args string) llm.ToolCall {
	var tc llm.ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// TestPauseCalls_GatesOnAskUserName pins llm_agent_pause.go:36 — the
// `if call.Function.Name != askUserToolName { continue }` guard. A tool that
// returns the pause sentinel but is NOT named ask_user must be SKIPPED by
// pauseCalls (never pre-executed, never treated as a pause). Removing the guard
// (the surviving mutant) would let the non-ask_user sentinel through and produce
// a spurious pause, so this assertion kills it.
func TestPauseCalls_GatesOnAskUserName(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(fakePauseTool{name: "not_ask_user"})
	a := newBareAgent(t, r)

	calls := []llm.ToolCall{toolCall("x1", "not_ask_user", `{}`)}
	pauses := a.pauseCalls(calls)
	if len(pauses) != 0 {
		t.Fatalf("a non-ask_user tool returning the pause sentinel must NOT be a pause; got %d pauses", len(pauses))
	}

	// And the positive control: under the canonical ask_user name the SAME
	// sentinel-returning tool IS detected, proving the gate keys on the name.
	r2 := tools.NewRegistry()
	r2.Register(fakePauseTool{name: askUserToolName})
	a2 := newBareAgent(t, r2)
	ok := a2.pauseCalls([]llm.ToolCall{toolCall("x2", askUserToolName, `{}`)})
	if len(ok) != 1 || ok[0].pause.ToolCallID != "x2" {
		t.Fatalf("ask_user-named sentinel tool must be detected as a pause carrying its call id; got %+v", ok)
	}
}

// TestDetectPause_UnknownTool pins llm_agent_pause.go:71 — the registry-miss
// `if !ok { return nil, false }`. Calling detectPause for a tool name that is NOT
// in the registry must return (nil, false) and never dereference the nil tool.
// Removing the early return (the surviving mutant) would call nilTool.Execute and
// panic, so a no-panic + (nil,false) assertion kills it.
func TestDetectPause_UnknownTool(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry()) // empty registry: ask_user absent
	call := toolCall("c", askUserToolName, `{}`)

	pause, ok := a.detectPause(call)
	if ok {
		t.Fatalf("a tool absent from the registry must not detect as a pause; ok=true")
	}
	if pause != nil {
		t.Fatalf("registry-miss must return a nil sentinel, got %+v", pause)
	}
}

// TestEmitPauses_StopsOnYieldFalse pins llm_agent_pause.go:92 — the
// `if !yield(...) { return }` guard. A consumer that returns false on the FIRST
// yield must see exactly one Event; the loop must not yield the second pause.
// Removing the early return (the surviving mutant) would yield all pauses
// regardless, so asserting the second yield never fires kills it.
func TestEmitPauses_StopsOnYieldFalse(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(tools.AskUser{})
	a := newBareAgent(t, r)

	pauses := []pauseCall{
		{pause: &tools.ErrAwaitingUserInput{Question: "q1", Kind: tools.KindApproval, ToolCallID: "p1"}},
		{pause: &tools.ErrAwaitingUserInput{Question: "q2", Kind: tools.KindApproval, ToolCallID: "p2"}},
	}

	var yielded []string
	var span [8]byte
	a.emitPauses(internalPauseIC(t), span, nil, pauses, func(ev *Event, _ error) bool {
		yielded = append(yielded, ev.Actions.AwaitingInput.ToolCallID)
		return false // stop after the first
	})

	if len(yielded) != 1 || yielded[0] != "p1" {
		t.Fatalf("consumer stopped after first yield: want exactly [p1], got %v", yielded)
	}
}

// TestEmitPauses_YieldsAllWhenConsumerContinues is the positive control for the
// same guard: a consumer that always returns true must receive BOTH pauses in
// emit order. Together with the stop-on-false test, both arms of the yield guard
// are pinned.
func TestEmitPauses_YieldsAllWhenConsumerContinues(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(tools.AskUser{})
	a := newBareAgent(t, r)

	pauses := []pauseCall{
		{pause: &tools.ErrAwaitingUserInput{Question: "q1", Kind: tools.KindApproval, ToolCallID: "p1"}},
		{pause: &tools.ErrAwaitingUserInput{Question: "q2", Kind: tools.KindApproval, ToolCallID: "p2"}},
	}

	var yielded []string
	var span [8]byte
	a.emitPauses(internalPauseIC(t), span, nil, pauses, func(ev *Event, _ error) bool {
		yielded = append(yielded, ev.Actions.AwaitingInput.ToolCallID)
		return true
	})

	if len(yielded) != 2 || yielded[0] != "p1" || yielded[1] != "p2" {
		t.Fatalf("a non-stopping consumer must see both pauses [p1 p2], got %v", yielded)
	}
}

// TestPauseEvent_CarriesProxiedIDs pins the D-05 projection: the optional
// proxied_from_child_id + proxied_tool_call_id on the ErrAwaitingUserInput
// sentinel must flow onto the Actions.AwaitingInput Event via pauseEvent. A
// direct (non-proxied) pause leaves both Event fields empty.
func TestPauseEvent_CarriesProxiedIDs(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	var span [8]byte

	proxied := &tools.ErrAwaitingUserInput{
		Question:           "relay?",
		Kind:               tools.KindClarification,
		ToolCallID:         "p1",
		ProxiedFromChildID: "11111111-1111-1111-1111-111111111111",
		ProxiedToolCallID:  "child-tc",
	}
	ev := a.pauseEvent(internalPauseIC(t), span, nil, proxied)
	ai := ev.Actions.AwaitingInput
	if ai == nil {
		t.Fatal("pauseEvent must set Actions.AwaitingInput")
	}
	if ai.ProxiedFromChildID != "11111111-1111-1111-1111-111111111111" || ai.ProxiedToolCallID != "child-tc" {
		t.Errorf("proxied ids not projected onto the Event: from=%q tc=%q",
			ai.ProxiedFromChildID, ai.ProxiedToolCallID)
	}

	direct := &tools.ErrAwaitingUserInput{Question: "q", Kind: tools.KindApproval, ToolCallID: "d1"}
	devent := a.pauseEvent(internalPauseIC(t), span, nil, direct)
	if dai := devent.Actions.AwaitingInput; dai.ProxiedFromChildID != "" || dai.ProxiedToolCallID != "" {
		t.Errorf("a direct pause must leave proxied Event fields empty, got from=%q tc=%q",
			dai.ProxiedFromChildID, dai.ProxiedToolCallID)
	}
}

// TestPauseOptions_EmptyVsPopulated pins llm_agent_pause.go:120 — the
// `if len(opts) == 0 { return nil }` early return. The nil-return arm is observed
// here as len==0; the populated arm must map labels/values 1:1.
func TestPauseOptions_EmptyVsPopulated(t *testing.T) {
	// Contract: no options returns a nil slice (not an allocated empty slice).
	// The nil arm matters for callers/log clarity even though the wire form is
	// identical under `omitempty`; pinning == nil locks the documented contract.
	if got := pauseOptions(nil); got != nil {
		t.Fatalf("no options must return a nil slice, got non-nil %#v", got)
	}
	if got := pauseOptions([]tools.Option{}); got != nil {
		t.Fatalf("an empty (non-nil) options slice must also return nil, got %#v", got)
	}

	opts := []tools.Option{{Label: "yes", Value: "y"}, {Label: "no", Value: "n"}}
	got := pauseOptions(opts)
	if len(got) != 2 {
		t.Fatalf("want 2 mapped options, got %d", len(got))
	}
	if got[0].Label != "yes" || got[0].Value != "y" || got[1].Label != "no" || got[1].Value != "n" {
		t.Fatalf("options must map label/value 1:1, got %+v", got)
	}
}
