//go:build spike_agui

// Spike 015 — agui-event-surface.
// Enumerates the SDK's core/events surface against the PRD Slice-8 acceptance
// list, golden-dumps the JSON wire shape of every required event, and probes
// the interrupt/resume contract (RunFinishedOutcome + RunAgentInput.Resume).
//
// Run: go run -tags spike_agui ./.planning/spikes/015-agui-event-surface
// Artifact: .planning/spikes/015-agui-event-surface/golden-events.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

func logf(cat, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, args...))
}

type namedEvent struct {
	name string
	ev   events.Event
}

func main() {
	failures := 0
	fail := func(format string, args ...any) {
		logf("FAIL", format, args...)
		failures++
	}

	// --- 1. PRD-required event set: construct + Validate + marshal each ---
	required := []namedEvent{
		{"RUN_STARTED", events.NewRunStartedEvent("thread-1", "run-1")},
		{"STEP_STARTED", events.NewStepStartedEvent("step-1")},
		{"STEP_FINISHED", events.NewStepFinishedEvent("step-1")},
		{"TEXT_MESSAGE_START", events.NewTextMessageStartEvent("msg-1")},
		{"TEXT_MESSAGE_CONTENT", events.NewTextMessageContentEvent("msg-1", "Ciao")},
		{"TEXT_MESSAGE_END", events.NewTextMessageEndEvent("msg-1")},
		{"TOOL_CALL_START", events.NewToolCallStartEvent("call-1", "web_search")},
		{"TOOL_CALL_ARGS", events.NewToolCallArgsEvent("call-1", `{"query":"meteo`)},
		{"TOOL_CALL_END", events.NewToolCallEndEvent("call-1")},
		{"TOOL_CALL_RESULT", events.NewToolCallResultEvent("msg-2", "call-1", "result preview + footer")},
		{"MESSAGES_SNAPSHOT", events.NewMessagesSnapshotEvent([]events.Message{
			{ID: "m1", Role: "user", Content: "ciao"},
			{ID: "m2", Role: "assistant", Content: "4", ToolCalls: nil},
			{ID: "m3", Role: "tool", Content: "42", ToolCallID: "call-1"},
		})},
		{"STATE_SNAPSHOT", events.NewStateSnapshotEvent(map[string]any{"cost_usd": 0.0})},
		{"STATE_DELTA", events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: 0.0042},
		})},
		{"RUN_FINISHED(success)", events.NewRunFinishedEventWithOptions("thread-1", "run-1",
			events.WithSuccessOutcome())},
		{"RUN_FINISHED(interrupt)", events.NewRunFinishedEventWithOptions("thread-1", "run-1",
			events.WithInterruptOutcome([]types.Interrupt{{
				ID:         "int-1",
				Reason:     "tool_call",
				Message:    "Serve conferma per procedere",
				ToolCallID: "call-ask-1",
				ResponseSchema: map[string]any{
					"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
				},
			}}))},
		{"RUN_ERROR", events.NewRunErrorEvent("LLM 5xx finale")},
		{"REASONING_START", events.NewReasoningStartEvent("rsn-1")},
		{"REASONING_MESSAGE_START", events.NewReasoningMessageStartEvent("rsn-1", "assistant")},
		{"REASONING_MESSAGE_CONTENT", events.NewReasoningMessageContentEvent("rsn-1", "thinking…")},
		{"REASONING_MESSAGE_END", events.NewReasoningMessageEndEvent("rsn-1")},
		{"REASONING_END", events.NewReasoningEndEvent("rsn-1")},
	}

	golden := make(map[string]json.RawMessage, len(required))
	for _, ne := range required {
		if err := ne.ev.Validate(); err != nil {
			fail("%s Validate: %v", ne.name, err)
			continue
		}
		raw, err := json.Marshal(ne.ev)
		if err != nil {
			fail("%s marshal: %v", ne.name, err)
			continue
		}
		golden[ne.name] = raw
		logf("WIRE", "%s %s", ne.name, raw)
	}

	// --- 2. RunAgentInput: parse the PRD smoke payload (camelCase) ---
	prdSmoke := `{"threadId":"c0ffee00-1111-2222-3333-444444444444","runId":"run-1",` +
		`"messages":[{"id":"m1","role":"user","content":"ciao"}]}`
	var in types.RunAgentInput
	if err := json.Unmarshal([]byte(prdSmoke), &in); err != nil {
		fail("RunAgentInput camelCase parse: %v", err)
	} else if in.ThreadID == "" || len(in.Messages) != 1 || in.Messages[0].Content != "ciao" {
		fail("RunAgentInput camelCase fields wrong: %+v", in)
	} else {
		logf("INPUT", "camelCase parse OK: threadId=%s messages=%d", in.ThreadID, len(in.Messages))
	}

	// --- 3. RunAgentInput: snake_case compat + resume array ---
	snake := `{"thread_id":"t1","run_id":"r2","resume":[{"interrupt_id":"int-1","status":"resolved","payload":{"answer":"sì"}}]}`
	var in2 types.RunAgentInput
	if err := json.Unmarshal([]byte(snake), &in2); err != nil {
		fail("RunAgentInput snake_case parse: %v", err)
	} else if in2.ThreadID != "t1" || len(in2.Resume) != 1 || in2.Resume[0].InterruptID != "int-1" ||
		in2.Resume[0].Status != types.ResumeStatusResolved {
		fail("RunAgentInput snake_case/resume fields wrong: %+v", in2)
	} else {
		logf("INPUT", "snake_case + resume[] parse OK: %+v", in2.Resume[0])
	}

	// --- 4. Outcome wire assertions (PRD wording vs SDK reality) ---
	var fin struct {
		Outcome *struct {
			Type       string            `json:"type"`
			Interrupts []types.Interrupt `json:"interrupts"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(golden["RUN_FINISHED(interrupt)"], &fin); err != nil || fin.Outcome == nil {
		fail("RUN_FINISHED(interrupt) outcome decode: %v", err)
	} else {
		logf("OUTCOME", "outcome.type=%q (PRD says \"interrupted\" — SDK says %q), interrupts=%d, responseSchema present=%t",
			fin.Outcome.Type, fin.Outcome.Type, len(fin.Outcome.Interrupts), fin.Outcome.Interrupts[0].ResponseSchema != nil)
	}

	// --- 5. Golden artifact ---
	out, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		fail("golden marshal: %v", err)
	} else if err := os.WriteFile(".planning/spikes/015-agui-event-surface/golden-events.json", out, 0o644); err != nil {
		fail("golden write: %v", err)
	} else {
		logf("ARTIFACT", "golden-events.json written (%d events)", len(golden))
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d failures", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED — all %d PRD-required events construct+validate+marshal; RunAgentInput parses camelCase+snake_case+resume[]", len(required))
}
