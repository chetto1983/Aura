package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningRecallExplicitOnly(t *testing.T) {
	ordinary := []MemoryRecallInput{
		{Mode: "semantic", Query: "deployment"},
		{Mode: "recent", Limit: 2},
		{Mode: "open", ConversationID: "conversation-a", AnchorSeq: 7, Limit: 2},
		{Mode: "scroll", Cursor: "invalid-hostile-cursor"},
	}
	for _, input := range ordinary {
		client, rec := newRecordingDB(t, `{"result":[]}`)
		_, _, _ = memoryRecallHandler(singleTenant(t, client))(
			context.Background(), reqWithIdentity(testIdentity), input)
		if statement := firstReasoningStatement(rec); statement != "" {
			t.Fatalf("ordinary mode %q queried reasoning: %s", input.Mode, statement)
		}
	}

	search := `{"result":[{"identity_id":"` + testIdentity + `","trace_id":"trace-a",` +
		`"source_ref":"postgres://aura/conversations/conversation-a/turns/7",` +
		`"conversation_id":"conversation-a","turn_seq":7,` +
		`"provider_summary":"Checked the deployment and reported the observed status.",` +
		`"status":"succeeded","created_at":"2026-09-01T00:00:00Z",` +
		`"terminal_at":"2026-09-01T00:01:00Z","expires_at":"2026-10-01T00:01:00Z"}]}`
	client, rec := newRecordingDB(t, search)
	_, output, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), reqWithIdentity(testIdentity),
		MemoryRecallInput{Mode: "reasoning", Query: "deployment", Limit: 5})
	if err != nil {
		t.Fatalf("explicit reasoning recall: %v", err)
	}
	if output.Abstained || output.Retrieval.ReasoningCount != 1 ||
		len(output.Evidence) != 1 || output.Evidence[0].Kind != "reasoning" {
		t.Fatalf("explicit reasoning output = %+v", output)
	}
	if statement := firstReasoningStatement(rec); statement == "" {
		t.Fatal("explicit reasoning mode did not query reasoning storage")
	}
	payload, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "Checked the deployment") {
		t.Fatalf("provider-visible summary missing: %s", payload)
	}
}

func TestReasoningRecallIdentity(t *testing.T) {
	t.Run("missing OAuth identity fails before reasoning access", func(t *testing.T) {
		client, rec := newRecordingDB(t)
		_, _, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), reqWithIdentity(""),
			MemoryRecallInput{Mode: "reasoning", Query: "deployment"})
		if err == nil {
			t.Fatal("missing identity was accepted")
		}
		if len(rec.statements) != 0 {
			t.Fatalf("missing identity reached reasoning storage: %v", rec.statements)
		}
	})

	t.Run("exact trace traversal stays owner scoped", func(t *testing.T) {
		trace := `{"result":[{"identity_id":"` + testIdentity + `","trace_id":"trace-a",` +
			`"source_ref":"postgres://aura/conversations/conversation-a/turns/7",` +
			`"conversation_id":"conversation-a","turn_seq":7,"provider_summary":"Deployment check.",` +
			`"status":"succeeded","created_at":"2026-09-01T00:00:00Z"}]}`
		steps := `{"result":[{"identity_id":"` + testIdentity + `","trace_id":"trace-a",` +
			`"step_index":1,"provider_summary":"Inspected status.","created_at":"2026-09-01T00:00:10Z"}]}`
		tools := `{"result":[{"step_index":1,"identity_id":"` + testIdentity + `",` +
			`"trace_id":"trace-a","call_id":"call-a","tool_name":"shell_exec",` +
			`"status":"succeeded","duration_ms":42,"argument_digest":"` + strings.Repeat("a", 64) + `",` +
			`"observation":"deployment is healthy","artifact_refs":["artifact://run-a/report.txt"],` +
			`"entity_refs":["deployment-a"],` +
			`"source_ref":"postgres://aura/conversations/conversation-a/turns/7"}]}`
		client, rec := newRecordingDB(t, trace, steps, tools)

		var input MemoryRecallInput
		if err := json.Unmarshal([]byte(`{"mode":"reasoning","trace_id":"trace-a"}`), &input); err != nil {
			t.Fatal(err)
		}
		_, output, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), reqWithIdentity(testIdentity), input)
		if err != nil {
			t.Fatalf("exact reasoning recall: %v", err)
		}
		payload, err := json.Marshal(output)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{
			`"trace_id":"trace-a"`, `"provider_summary":"Inspected status."`,
			`"tool_name":"shell_exec"`, `"duration_ms":42`,
			`"argument_digest":"` + strings.Repeat("a", 64) + `"`,
			`"artifact_refs":["artifact://run-a/report.txt"]`,
			`"entity_refs":["deployment-a"]`,
		} {
			if !strings.Contains(string(payload), required) {
				t.Errorf("exact reasoning output missing %s: %s", required, payload)
			}
		}
		for index, statement := range rec.statements {
			if strings.Contains(statement, "Reasoning") && rec.params[index]["identity_id"] != testIdentity {
				t.Fatalf("reasoning query lacks owner param: %q %#v", statement, rec.params[index])
			}
		}
	})
}

func firstReasoningStatement(rec *recordingDB) string {
	for _, statement := range rec.statements {
		if strings.Contains(statement, "ReasoningTrace") ||
			strings.Contains(statement, "ReasoningStep") ||
			strings.Contains(statement, "ReasoningToolCall") {
			return statement
		}
	}
	return ""
}
