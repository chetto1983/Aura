package arcadedb

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReasoningSchemaStatements(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}

	for _, statement := range expectedReasoningSchemaStatements() {
		if count := countString(rec.statements, statement); count != 1 {
			t.Errorf("reasoning schema statement %q emitted %d times, want once", statement, count)
		}
		if !strings.Contains(statement, "IF NOT EXISTS") {
			t.Errorf("reasoning schema statement is not replay-safe: %s", statement)
		}
	}

	all := rec.joined()
	for _, forbidden := range []string{"chain_of_thought", "hidden_reasoning", "raw_output", "raw_arguments"} {
		if strings.Contains(strings.ToLower(all), forbidden) {
			t.Errorf("reasoning schema exposes forbidden field %q", forbidden)
		}
	}
}

func TestEnsureMemorySchemaRegistersReasoningSchema(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	for attempt := range 2 {
		if err := client.EnsureMemorySchema(context.Background()); err != nil {
			t.Fatalf("EnsureMemorySchema attempt %d: %v", attempt+1, err)
		}
	}

	want := expectedReasoningSchemaStatements()
	for _, statement := range want {
		if count := countString(rec.statements, statement); count != 2 {
			t.Errorf("reasoning schema statement %q emitted %d times, want once per initialization", statement, count)
		}
	}

	lastConversation := lastStringIndex(rec.statements,
		conversationSchemaStatements()[len(conversationSchemaStatements())-1])
	firstReasoning := lastStringIndex(rec.statements, want[0])
	if firstReasoning <= lastConversation {
		t.Fatalf("reasoning schema must follow ordinary memory schema: conversation=%d reasoning=%d",
			lastConversation, firstReasoning)
	}
}

func expectedReasoningSchemaStatements() []string {
	return []string{
		"CREATE VERTEX TYPE ReasoningTrace IF NOT EXISTS",
		"CREATE PROPERTY ReasoningTrace.identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.source_ref IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.conversation_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.turn_seq IF NOT EXISTS INTEGER (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.provider_summary IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.status IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.created_at IF NOT EXISTS DATETIME (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningTrace.terminal_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY ReasoningTrace.expires_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY ReasoningTrace.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (identity_id, trace_id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (identity_id, source_ref) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (identity_id, status) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (expires_at) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (provider_summary) FULL_TEXT METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (embedding) LSM_VECTOR METADATA { \"dimensions\": 768, \"similarity\": \"COSINE\", \"quantization\": \"NONE\" }",

		"CREATE VERTEX TYPE ReasoningStep IF NOT EXISTS",
		"CREATE PROPERTY ReasoningStep.identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningStep.trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningStep.step_index IF NOT EXISTS INTEGER (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningStep.provider_summary IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningStep.created_at IF NOT EXISTS DATETIME (MANDATORY TRUE)",
		"CREATE INDEX IF NOT EXISTS ON ReasoningStep (identity_id, trace_id, step_index) UNIQUE",

		"CREATE VERTEX TYPE ReasoningToolCall IF NOT EXISTS",
		"CREATE PROPERTY ReasoningToolCall.identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningToolCall.trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningToolCall.call_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningToolCall.tool_name IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningToolCall.status IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY ReasoningToolCall.duration_ms IF NOT EXISTS LONG",
		"CREATE PROPERTY ReasoningToolCall.argument_digest IF NOT EXISTS STRING",
		"CREATE PROPERTY ReasoningToolCall.observation IF NOT EXISTS STRING",
		"CREATE PROPERTY ReasoningToolCall.artifact_refs IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY ReasoningToolCall.entity_refs IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY ReasoningToolCall.source_ref IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE INDEX IF NOT EXISTS ON ReasoningToolCall (identity_id, trace_id, call_id) UNIQUE",

		"CREATE EDGE TYPE INITIATED_BY IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON INITIATED_BY (`@out`, `@in`) UNIQUE",
		"CREATE EDGE TYPE HAS_STEP IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON HAS_STEP (`@out`, `@in`) UNIQUE",
		"CREATE EDGE TYPE NEXT IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON NEXT (`@out`, `@in`) UNIQUE",
		"CREATE EDGE TYPE INVOKED IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON INVOKED (`@out`, `@in`) UNIQUE",
		"CREATE EDGE TYPE TOUCHED IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON TOUCHED (`@out`, `@in`) UNIQUE",
	}
}

func TestReasoningToolMetadataBounded(t *testing.T) {
	t.Run("provider formatting is canonicalized at the storage boundary", func(t *testing.T) {
		trace := validReasoningTrace()
		trace.ProviderSummary = "Inspect deployment\n\tcompare status\x00"
		trace.Steps[0].ProviderSummary = "Inspect deployment\r\ncompare status"
		trace.Steps[0].ToolCalls[0].Observation = "line one\nline two"

		normalized, err := normalizeReasoningTrace(trace)
		if err != nil {
			t.Fatalf("normalizeReasoningTrace: %v", err)
		}
		if normalized.ProviderSummary != "Inspect deployment compare status" {
			t.Fatalf("provider summary = %q", normalized.ProviderSummary)
		}
		if normalized.Steps[0].ProviderSummary != "Inspect deployment compare status" {
			t.Fatalf("step summary = %q", normalized.Steps[0].ProviderSummary)
		}
		if normalized.Steps[0].ToolCalls[0].Observation != "line one line two" {
			t.Fatalf("observation = %q", normalized.Steps[0].ToolCalls[0].Observation)
		}
	})

	t.Run("tool observation is optional", func(t *testing.T) {
		trace := validReasoningTrace()
		trace.Steps[0].ToolCalls[0].Observation = ""
		if _, err := normalizeReasoningTrace(trace); err != nil {
			t.Fatalf("normalizeReasoningTrace: %v", err)
		}
	})

	t.Run("valid evidence is redacted and stored without unrestricted fields", func(t *testing.T) {
		client, rec := recordingClient(t, `{"result":[]}`)
		trace := validReasoningTrace()
		trace.Steps[0].ToolCalls[0].Observation =
			"request failed with Authorization: Bearer sk-or-v1abcdef1234567890ghij"
		if err := client.UpsertReasoningTrace(context.Background(), trace); err != nil {
			t.Fatalf("UpsertReasoningTrace: %v", err)
		}
		payload, err := json.Marshal(rec.params)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "sk-or-v1abcdef1234567890ghij") {
			t.Fatalf("reasoning write leaked a credential: %s", payload)
		}
		if !strings.Contains(string(payload), "[REDACTED]") {
			t.Fatalf("reasoning write did not preserve a redaction marker: %s", payload)
		}
	})

	for _, test := range []struct {
		name    string
		mutate  func(*ReasoningTrace)
		wantErr string
	}{
		{
			name: "observation over cap",
			mutate: func(trace *ReasoningTrace) {
				trace.Steps[0].ToolCalls[0].Observation = strings.Repeat("x", reasoningObservationRunes+1)
			},
			wantErr: "observation exceeds",
		},
		{
			name: "embedded blob",
			mutate: func(trace *ReasoningTrace) {
				trace.Steps[0].ToolCalls[0].Observation = "data:application/octet-stream;base64,AAAA"
			},
			wantErr: "blob",
		},
		{
			name: "unrestricted argument text instead of digest",
			mutate: func(trace *ReasoningTrace) {
				trace.Steps[0].ToolCalls[0].ArgumentDigest = `{"command":"cat /secret"}`
			},
			wantErr: "argument_digest",
		},
		{
			name: "unvalidated artifact reference",
			mutate: func(trace *ReasoningTrace) {
				trace.Steps[0].ToolCalls[0].ArtifactRefs = []string{"../../secret.txt"}
			},
			wantErr: "artifact_ref",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			trace := validReasoningTrace()
			test.mutate(&trace)
			err := client.UpsertReasoningTrace(context.Background(), trace)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
			if len(rec.statements) != 0 {
				t.Fatalf("invalid reasoning evidence reached storage: %v", rec.statements)
			}
		})
	}

	for _, model := range []reflect.Type{
		reflect.TypeFor[ReasoningTrace](),
		reflect.TypeFor[ReasoningStep](),
		reflect.TypeFor[ReasoningToolCall](),
	} {
		for fieldIndex := range model.NumField() {
			field := strings.ToLower(model.Field(fieldIndex).Name)
			for _, forbidden := range []string{"chain", "raw", "secret", "arguments", "blob"} {
				if strings.Contains(field, forbidden) {
					t.Errorf("%s exposes forbidden field %s", model.Name(), model.Field(fieldIndex).Name)
				}
			}
		}
	}
}

func TestReasoningGraphTouchedEntities(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[{"name":"deployment-a"}]}`)
	trace := validReasoningTrace()
	trace.Steps[0].ToolCalls[0].EntityRefs = []string{"deployment-a", "missing-a"}
	if err := client.UpsertReasoningTrace(context.Background(), trace); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}

	var stored []any
	var touched []string
	resolved := false
	for index, statement := range rec.statements {
		switch {
		case strings.Contains(statement, "SELECT name FROM Entity"):
			resolved = true
		case strings.HasPrefix(statement, "UPDATE "+reasoningToolCallType):
			stored, _ = rec.params[index]["entity_refs"].([]any)
		case strings.HasPrefix(statement, "CREATE EDGE TOUCHED"):
			touched = append(touched, rec.params[index]["entity_name"].(string))
		}
	}
	if !resolved {
		t.Fatal("entity candidates were not resolved against the graph")
	}
	if len(stored) != 1 || stored[0] != "deployment-a" {
		t.Fatalf("stored entity refs = %#v, want only the existing entity", stored)
	}
	if len(touched) != 1 || touched[0] != "deployment-a" {
		t.Fatalf("TOUCHED targets = %#v, want only the existing entity", touched)
	}
}

func TestReasoningRecallIdentity(t *testing.T) {
	t.Run("missing identity fails before database access", func(t *testing.T) {
		client, rec := recordingClient(t, `{"result":[]}`)
		_, err := client.SearchReasoningTraces(context.Background(), "", "deployment", 5)
		if err == nil || !strings.Contains(err.Error(), "identity") {
			t.Fatalf("error = %v, want identity refusal", err)
		}
		if len(rec.statements) != 0 {
			t.Fatalf("missing identity reached storage: %v", rec.statements)
		}
	})

	t.Run("foreign search rows are discarded", func(t *testing.T) {
		client, rec := recordingClient(t, `{"result":[{"identity_id":"identity-b","trace_id":"trace-b","source_ref":"postgres://aura/conversations/c-b/turns/1","conversation_id":"c-b","turn_seq":1,"provider_summary":"foreign","status":"succeeded","created_at":"2026-09-01T00:00:00Z"}]}`)
		traces, err := client.SearchReasoningTraces(context.Background(), "identity-a", "deployment", 5)
		if err != nil {
			t.Fatalf("SearchReasoningTraces: %v", err)
		}
		if len(traces) != 0 {
			t.Fatalf("foreign traces returned: %+v", traces)
		}
		statement, params, ok := findRecordedStatement(rec, reasoningTraceType)
		if !ok || !strings.Contains(statement, "identity_id = :identity_id") || params["identity_id"] != "identity-a" {
			t.Fatalf("reasoning search lacks owner predicate: %q %#v", statement, params)
		}
	})
}

func validReasoningTrace() ReasoningTrace {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return ReasoningTrace{
		IdentityID: "identity-a", TraceID: "trace-a",
		SourceRef:      "postgres://aura/conversations/conversation-a/turns/7",
		ConversationID: "conversation-a", TurnSeq: 7,
		ProviderSummary: "Checked the deployment and reported the observed status.",
		Status:          ReasoningStatusSucceeded, CreatedAt: created, TerminalAt: created.Add(time.Minute),
		Steps: []ReasoningStep{{
			Index: 1, ProviderSummary: "Inspected the deployment status.", CreatedAt: created,
			ToolCalls: []ReasoningToolCall{{
				CallID: "call-a", ToolName: "shell_exec", Status: "succeeded", DurationMillis: 42,
				ArgumentDigest: strings.Repeat("a", reasoningDigestRunes),
				Observation:    "deployment is healthy", ArtifactRefs: []string{"artifact://run-a/report.txt"},
				EntityRefs: []string{"deployment-a"},
				SourceRef:  "postgres://aura/conversations/conversation-a/turns/7",
			}},
		}},
	}
}

func findRecordedStatement(rec *recorder, fragment string) (string, map[string]any, bool) {
	for index, statement := range rec.statements {
		if strings.Contains(statement, fragment) {
			return statement, rec.params[index], true
		}
	}
	return "", nil, false
}

func TestReasoningTerminalExpiry(t *testing.T) {
	terminal := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	policy := ReasoningRetentionPolicy{
		SuccessTTL: 30 * 24 * time.Hour,
		FailedTTL:  7 * 24 * time.Hour,
	}
	for _, test := range []struct {
		status ReasoningStatus
		want   time.Duration
	}{
		{status: ReasoningStatusSucceeded, want: 30 * 24 * time.Hour},
		{status: ReasoningStatusFailed, want: 7 * 24 * time.Hour},
		{status: ReasoningStatusCancelled, want: 7 * 24 * time.Hour},
	} {
		t.Run(string(test.status), func(t *testing.T) {
			trace := validReasoningTrace()
			trace.Status, trace.TerminalAt = test.status, terminal
			got, err := trace.SetTerminalExpiry(policy, time.Time{})
			if err != nil {
				t.Fatalf("SetTerminalExpiry: %v", err)
			}
			if got.ExpiresAt.Sub(terminal) != test.want {
				t.Fatalf("expiry = %s, want terminal + %s", got.ExpiresAt, test.want)
			}
		})
	}

	t.Run("earlier source expiry always wins", func(t *testing.T) {
		trace := validReasoningTrace()
		trace.TerminalAt = terminal
		trace.ExpiresAt = terminal.Add(4 * 24 * time.Hour)
		sourceExpiry := terminal.Add(2 * 24 * time.Hour)
		got, err := trace.SetTerminalExpiry(policy, sourceExpiry)
		if err != nil {
			t.Fatalf("SetTerminalExpiry: %v", err)
		}
		if !got.ExpiresAt.Equal(sourceExpiry) {
			t.Fatalf("expiry = %s, want source cap %s", got.ExpiresAt, sourceExpiry)
		}
	})

	t.Run("existing shorter expiry never refreshes", func(t *testing.T) {
		trace := validReasoningTrace()
		trace.TerminalAt = terminal
		trace.ExpiresAt = terminal.Add(3 * 24 * time.Hour)
		got, err := trace.SetTerminalExpiry(policy, terminal.Add(20*24*time.Hour))
		if err != nil {
			t.Fatalf("SetTerminalExpiry: %v", err)
		}
		if !got.ExpiresAt.Equal(trace.ExpiresAt) {
			t.Fatalf("existing expiry refreshed from %s to %s", trace.ExpiresAt, got.ExpiresAt)
		}
	})

	t.Run("existing expiry cannot widen the status class", func(t *testing.T) {
		trace := validReasoningTrace()
		trace.TerminalAt = terminal
		trace.ExpiresAt = terminal.Add(31 * 24 * time.Hour)
		if _, err := trace.SetTerminalExpiry(policy, time.Time{}); err == nil {
			t.Fatal("expiry longer than the successful class was accepted")
		}
	})

	for _, test := range []struct {
		name   string
		policy ReasoningRetentionPolicy
	}{
		{name: "zero success", policy: ReasoningRetentionPolicy{FailedTTL: 7 * 24 * time.Hour}},
		{name: "negative failed", policy: ReasoningRetentionPolicy{SuccessTTL: 30 * 24 * time.Hour, FailedTTL: -time.Hour}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := validReasoningTrace()
			trace.TerminalAt = terminal
			if _, err := trace.SetTerminalExpiry(test.policy, time.Time{}); err == nil {
				t.Fatal("invalid retention policy was accepted")
			}
		})
	}

	t.Run("read does not extend persisted expiry", func(t *testing.T) {
		expiresAt := terminal.Add(3 * 24 * time.Hour)
		traceRow := `{"result":[{"identity_id":"identity-a","trace_id":"trace-a",` +
			`"source_ref":"postgres://aura/conversations/conversation-a/turns/7",` +
			`"conversation_id":"conversation-a","turn_seq":7,"provider_summary":"Deployment check.",` +
			`"status":"succeeded","created_at":"2026-09-01T00:00:00Z",` +
			`"terminal_at":"` + terminal.Format(time.RFC3339) + `","expires_at":"` + expiresAt.Format(time.RFC3339) + `"}]}`
		client, rec := recordingClient(t, traceRow)
		got, err := client.SearchReasoningTraces(context.Background(), "identity-a", "deployment", 1)
		if err != nil || len(got) != 1 {
			t.Fatalf("SearchReasoningTraces: count=%d err=%v", len(got), err)
		}
		if !got[0].ExpiresAt.Equal(expiresAt) {
			t.Fatalf("read expiry = %s, want %s", got[0].ExpiresAt, expiresAt)
		}
		for _, statement := range rec.statements {
			if strings.HasPrefix(strings.TrimSpace(statement), "UPDATE") {
				t.Fatalf("reasoning read refreshed expiry: %s", statement)
			}
		}
	})
}

// A trace found by SEARCH has to arrive as complete as one opened by id. It did
// not: measured 2026-09-03 through the memory MCP, all five searched traces came
// back with steps:[], so the audit could say Aura had reasoned about something
// and never what it did about it.
func TestSearchReasoningTracesCarriesStepsAndToolCalls(t *testing.T) {
	traceRow := `{"result":[{"@rid":"#40:0","identity_id":"identity-a","trace_id":"trace-1",` +
		`"source_ref":"postgres://aura/conversations/c1/turns/7","conversation_id":"c1","turn_seq":7,` +
		`"provider_summary":"Store what the operator said.","status":"succeeded",` +
		`"created_at":"2026-09-03T10:00:00Z"}]}`
	stepRow := `{"result":[{"identity_id":"identity-a","trace_id":"trace-1","step_index":1,` +
		`"provider_summary":"Store what the operator said.","created_at":"2026-09-03T10:00:00Z"}]}`
	toolRow := `{"result":[{"step_index":1,"identity_id":"identity-a","trace_id":"trace-1",` +
		`"call_id":"call-1","tool_name":"memory__memory_batch","status":"succeeded","duration_ms":250,` +
		`"argument_digest":"","observation":"","artifact_refs":[],"entity_refs":["Aura"],` +
		`"source_ref":"postgres://aura/conversations/c1/turns/7"}]}`
	// lexical search (no embedder), then the two set-shaped body reads.
	client, rec := recordingClient(t, traceRow, stepRow, toolRow)
	traces, err := client.SearchReasoningTraces(t.Context(), "identity-a", "operator", 5)
	if err != nil {
		t.Fatalf("SearchReasoningTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("traces = %#v", traces)
	}
	if len(traces[0].Steps) != 1 {
		t.Fatalf("searched trace came back without its steps:\n%s", rec.joined())
	}
	tools := traces[0].Steps[0].ToolCalls
	if len(tools) != 1 || tools[0].ToolName != "memory__memory_batch" {
		t.Fatalf("searched trace came back without its tool calls: %#v", tools)
	}
	if strings.Join(tools[0].EntityRefs, ",") != "Aura" {
		t.Fatalf("entity refs = %#v", tools[0].EntityRefs)
	}
	// One read per body, not one per trace: the set statements exist for that.
	if got := strings.Count(rec.joined(), "trace_ids"); got != 2 {
		t.Fatalf("body reads = %d, want one for steps and one for tools:\n%s", got, rec.joined())
	}
}
