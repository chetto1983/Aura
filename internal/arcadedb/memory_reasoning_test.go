package arcadedb

import (
	"context"
	"strings"
	"testing"
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
