package arcadedb

import "strconv"

const (
	reasoningTraceType    = "ReasoningTrace"
	reasoningStepType     = "ReasoningStep"
	reasoningToolCallType = "ReasoningToolCall"
)

// reasoningSchemaStatements owns the explicit-only reasoning memory schema.
// Required source and identity fields make orphaned or cross-tenant records
// unrepresentable; only provider-visible summaries and bounded tool evidence
// have storage columns.
func reasoningSchemaStatements() []string {
	return []string{
		"CREATE VERTEX TYPE " + reasoningTraceType + " IF NOT EXISTS",
		"CREATE PROPERTY " + reasoningTraceType + ".identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".source_ref IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".conversation_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".turn_seq IF NOT EXISTS INTEGER (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".provider_summary IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".status IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".created_at IF NOT EXISTS DATETIME (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningTraceType + ".terminal_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + reasoningTraceType + ".expires_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + reasoningTraceType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (identity_id, trace_id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (identity_id, source_ref) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (identity_id, status) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (expires_at) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (provider_summary) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningTraceType + " (embedding) LSM_VECTOR METADATA " +
			"{ \"dimensions\": " + strconv.Itoa(vectorDimensions) +
			", \"similarity\": \"COSINE\", \"quantization\": \"" + vectorQuantization + "\" }",

		"CREATE VERTEX TYPE " + reasoningStepType + " IF NOT EXISTS",
		"CREATE PROPERTY " + reasoningStepType + ".identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningStepType + ".trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningStepType + ".step_index IF NOT EXISTS INTEGER (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningStepType + ".provider_summary IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningStepType + ".created_at IF NOT EXISTS DATETIME (MANDATORY TRUE)",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningStepType + " (identity_id, trace_id, step_index) UNIQUE",

		"CREATE VERTEX TYPE " + reasoningToolCallType + " IF NOT EXISTS",
		"CREATE PROPERTY " + reasoningToolCallType + ".identity_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningToolCallType + ".trace_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningToolCallType + ".call_id IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningToolCallType + ".tool_name IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningToolCallType + ".status IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE PROPERTY " + reasoningToolCallType + ".duration_ms IF NOT EXISTS LONG",
		"CREATE PROPERTY " + reasoningToolCallType + ".argument_digest IF NOT EXISTS STRING",
		"CREATE PROPERTY " + reasoningToolCallType + ".observation IF NOT EXISTS STRING",
		"CREATE PROPERTY " + reasoningToolCallType + ".artifact_refs IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY " + reasoningToolCallType + ".entity_refs IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY " + reasoningToolCallType + ".source_ref IF NOT EXISTS STRING (MANDATORY TRUE)",
		"CREATE INDEX IF NOT EXISTS ON " + reasoningToolCallType + " (identity_id, trace_id, call_id) UNIQUE",

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
