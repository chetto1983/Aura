package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

const (
	reasoningSummaryRunes     = 4096
	reasoningObservationRunes = 1024
	reasoningDigestRunes      = 64
	reasoningReferenceRunes   = 256
	reasoningMaxSteps         = 64
	reasoningMaxToolsPerStep  = 32
	reasoningMaxReferences    = 32
	reasoningMaxSearchResults = 20
)

// ReasoningStatus is the persisted lifecycle state of one trace.
type ReasoningStatus string

const (
	// ReasoningStatusSucceeded identifies a successful terminal answer.
	ReasoningStatusSucceeded ReasoningStatus = "succeeded"
	// ReasoningStatusFailed identifies a terminal failed answer.
	ReasoningStatusFailed ReasoningStatus = "failed"
	// ReasoningStatusCancelled identifies a terminal cancelled answer.
	ReasoningStatusCancelled ReasoningStatus = "cancelled"
)

// ReasoningToolCall is bounded, redacted evidence for one invoked tool.
type ReasoningToolCall struct {
	CallID         string   `json:"call_id"`
	ToolName       string   `json:"tool_name"`
	Status         string   `json:"status"`
	DurationMillis int64    `json:"duration_ms,omitempty"`
	ArgumentDigest string   `json:"argument_digest,omitempty"`
	Observation    string   `json:"observation,omitempty"`
	ArtifactRefs   []string `json:"artifact_refs,omitempty"`
	EntityRefs     []string `json:"entity_refs,omitempty"`
	SourceRef      string   `json:"source_ref"`
}

// ReasoningStep is one ordered provider-visible part of a trace.
type ReasoningStep struct {
	Index           int                 `json:"index"`
	ProviderSummary string              `json:"provider_summary"`
	CreatedAt       time.Time           `json:"created_at"`
	ToolCalls       []ReasoningToolCall `json:"tool_calls,omitempty"`
}

// ReasoningTrace is one explicitly retrievable provider-visible reasoning graph.
type ReasoningTrace struct {
	IdentityID      string          `json:"identity_id"`
	TraceID         string          `json:"trace_id"`
	SourceRef       string          `json:"source_ref"`
	ConversationID  string          `json:"conversation_id"`
	TurnSeq         int             `json:"turn_seq"`
	ProviderSummary string          `json:"provider_summary"`
	Status          ReasoningStatus `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	TerminalAt      time.Time       `json:"terminal_at,omitzero"`
	ExpiresAt       time.Time       `json:"expires_at,omitzero"`
	Steps           []ReasoningStep `json:"steps,omitempty"`
}

// ReasoningRetentionPolicy carries the two exact terminal retention classes.
type ReasoningRetentionPolicy struct {
	SuccessTTL time.Duration
	FailedTTL  time.Duration
}

var defaultReasoningRetentionPolicy = ReasoningRetentionPolicy{
	SuccessTTL: 30 * 24 * time.Hour,
	FailedTTL:  7 * 24 * time.Hour,
}

// SetTerminalExpiry applies a terminal class without extending an existing or source cap.
func (trace ReasoningTrace) SetTerminalExpiry(
	policy ReasoningRetentionPolicy,
	sourceExpiry time.Time,
) (ReasoningTrace, error) {
	if policy.SuccessTTL <= 0 || policy.FailedTTL <= 0 {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning retention TTLs must be positive")
	}
	if trace.TerminalAt.IsZero() {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning terminal_at must be set before expiry")
	}
	var ttl time.Duration
	switch trace.Status {
	case ReasoningStatusSucceeded:
		ttl = policy.SuccessTTL
	case ReasoningStatusFailed, ReasoningStatusCancelled:
		ttl = policy.FailedTTL
	default:
		return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning status %q is not terminal", trace.Status)
	}
	classCap := trace.TerminalAt.Add(ttl)
	if !trace.ExpiresAt.IsZero() && trace.ExpiresAt.After(classCap) {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning expires_at exceeds its status retention class")
	}
	expiresAt := classCap
	if !trace.ExpiresAt.IsZero() && trace.ExpiresAt.Before(expiresAt) {
		expiresAt = trace.ExpiresAt
	}
	if !sourceExpiry.IsZero() && sourceExpiry.Before(expiresAt) {
		expiresAt = sourceExpiry
	}
	trace.ExpiresAt = expiresAt
	return trace, nil
}

// UpsertReasoningTrace persists one complete identity-scoped trace graph.
func (c *Client) UpsertReasoningTrace(ctx context.Context, trace ReasoningTrace) error {
	trace, err := trace.SetTerminalExpiry(defaultReasoningRetentionPolicy, time.Time{})
	if err != nil {
		return err
	}
	trace, err = normalizeReasoningTrace(trace)
	if err != nil {
		return err
	}
	vector := c.embedStatement(ctx, trace.ProviderSummary)
	session, err := c.beginTx(ctx)
	if err != nil {
		return err
	}
	defer c.rollbackTx(context.WithoutCancel(ctx), session)
	trace, err = c.resolveReasoningEntities(ctx, session, trace)
	if err != nil {
		return err
	}

	params := reasoningTraceParams(trace)
	statement := upsertReasoningTraceStatement
	if vector != nil {
		statement += ", embedding = :embedding"
		params["embedding"] = vector
	}
	statement += reasoningTraceWhere
	if _, err := c.commandInTx(ctx, session, statement, params); err != nil {
		return fmt.Errorf("arcadedb: upsert reasoning trace: %w", err)
	}
	if vector == nil {
		if _, err := c.commandInTx(ctx, session, clearReasoningEmbeddingStatement, params); err != nil {
			return fmt.Errorf("arcadedb: clear reasoning embedding: %w", err)
		}
	}
	if _, err := c.commandInTx(ctx, session, createReasoningInitiatorStatement, params); err != nil {
		return fmt.Errorf("arcadedb: link reasoning initiator: %w", err)
	}
	for stepOffset, step := range trace.Steps {
		stepParams := reasoningStepParams(trace, step)
		if _, err := c.commandInTx(ctx, session, upsertReasoningStepStatement, stepParams); err != nil {
			return fmt.Errorf("arcadedb: upsert reasoning step %d: %w", step.Index, err)
		}
		if _, err := c.commandInTx(ctx, session, createReasoningStepStatement, stepParams); err != nil {
			return fmt.Errorf("arcadedb: link reasoning step %d: %w", step.Index, err)
		}
		if stepOffset > 0 {
			if _, err := c.commandInTx(ctx, session, createNextReasoningStepStatement, stepParams); err != nil {
				return fmt.Errorf("arcadedb: order reasoning step %d: %w", step.Index, err)
			}
		}
		for _, tool := range step.ToolCalls {
			toolParams := reasoningToolParams(trace, step, tool)
			if _, err := c.commandInTx(ctx, session, upsertReasoningToolStatement, toolParams); err != nil {
				return fmt.Errorf("arcadedb: upsert reasoning tool %q: %w", tool.CallID, err)
			}
			if _, err := c.commandInTx(ctx, session, createInvokedReasoningToolStatement, toolParams); err != nil {
				return fmt.Errorf("arcadedb: link reasoning tool %q: %w", tool.CallID, err)
			}
			for _, entity := range tool.EntityRefs {
				toolParams["entity_name"] = entity
				if _, err := c.commandInTx(ctx, session, createReasoningTouchedStatement, toolParams); err != nil {
					return fmt.Errorf("arcadedb: link reasoning entity %q: %w", entity, err)
				}
			}
		}
	}
	if err := c.commitTx(ctx, session); err != nil {
		return fmt.Errorf("arcadedb: commit reasoning trace: %w", err)
	}
	return nil
}

const resolveReasoningEntitiesStatement = "SELECT name FROM Entity WHERE name IN :entity_names LIMIT 32"

func (c *Client) resolveReasoningEntities(
	ctx context.Context,
	session string,
	trace ReasoningTrace,
) (ReasoningTrace, error) {
	candidates := make(map[string]struct{})
	for _, step := range trace.Steps {
		for _, tool := range step.ToolCalls {
			for _, ref := range tool.EntityRefs {
				candidates[ref] = struct{}{}
			}
		}
	}
	if len(candidates) == 0 {
		return trace, nil
	}
	entityNames := make([]string, 0, len(candidates))
	for candidate := range candidates {
		entityNames = append(entityNames, candidate)
	}
	sort.Strings(entityNames)
	rows, err := c.queryInTx(ctx, session, resolveReasoningEntitiesStatement, map[string]any{
		"entity_names": entityNames,
	})
	if err != nil {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: resolve reasoning entities: %w", err)
	}
	existing := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if name := rowString(row, "name"); name != "" {
			existing[name] = struct{}{}
		}
	}
	for stepIndex := range trace.Steps {
		for toolIndex := range trace.Steps[stepIndex].ToolCalls {
			refs := trace.Steps[stepIndex].ToolCalls[toolIndex].EntityRefs
			validated := refs[:0]
			for _, ref := range refs {
				if _, ok := existing[ref]; ok {
					validated = append(validated, ref)
				}
			}
			trace.Steps[stepIndex].ToolCalls[toolIndex].EntityRefs = validated
		}
	}
	return trace, nil
}

// SearchReasoningTraces searches bounded trace summaries for one identity.
func (c *Client) SearchReasoningTraces(
	ctx context.Context,
	identityID, query string,
	limit int,
) ([]ReasoningTrace, error) {
	identityID, query = strings.TrimSpace(identityID), strings.TrimSpace(query)
	if identityID == "" {
		return nil, fmt.Errorf("arcadedb: reasoning search identity must be non-empty")
	}
	if query == "" {
		return nil, fmt.Errorf("arcadedb: reasoning search query must be non-empty")
	}
	if err := validateRuneLimit("reasoning search query", query, c.memoryLimits().QueryRunes); err != nil {
		return nil, err
	}
	limit = min(boundedLimit(limit, 5, c.memoryLimits().Results), reasoningMaxSearchResults)
	params := map[string]any{
		"identity_id": identityID, "query": escapeLucene(query), "limit": limit,
		"now": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if c.embedder == nil {
		return c.searchReasoningLexical(ctx, params)
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil || len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return c.searchReasoningLexical(ctx, params)
	}
	params["vector"] = vectors[0]
	params["candidates"] = min(max(limit*4, 20), c.memoryLimits().HybridCandidates)
	ranked, err := c.Query(ctx, searchReasoningHybridStatement, params)
	if err != nil {
		return c.searchReasoningLexical(ctx, params)
	}
	rids := make([]string, 0, len(ranked))
	for _, row := range ranked {
		if rid := strings.TrimSpace(fmt.Sprint(row["rid"])); rid != "" && rid != "<nil>" {
			rids = append(rids, rid)
		}
	}
	if len(rids) == 0 {
		return []ReasoningTrace{}, nil
	}
	params["rids"] = rids
	rows, err := c.Query(ctx, hydrateReasoningTracesStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: hydrate reasoning traces: %w", err)
	}
	order := make(map[string]int, len(rids))
	for index, rid := range rids {
		order[rid] = index
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return order[fmt.Sprint(rows[i]["@rid"])] < order[fmt.Sprint(rows[j]["@rid"])]
	})
	return reasoningTracesFromRows(rows, identityID, limit)
}

// GetReasoningTrace retrieves one bounded ordered graph for its owning identity.
func (c *Client) GetReasoningTrace(
	ctx context.Context,
	identityID, traceID string,
) (ReasoningTrace, bool, error) {
	identityID, traceID = strings.TrimSpace(identityID), strings.TrimSpace(traceID)
	if identityID == "" || traceID == "" {
		return ReasoningTrace{}, false, fmt.Errorf("arcadedb: reasoning trace identity and trace_id must be non-empty")
	}
	params := map[string]any{
		"identity_id": identityID, "trace_id": traceID,
		"now": time.Now().UTC().Format(time.RFC3339Nano),
	}
	rows, err := c.Query(ctx, getReasoningTraceStatement, params)
	if err != nil {
		return ReasoningTrace{}, false, fmt.Errorf("arcadedb: get reasoning trace: %w", err)
	}
	traces, err := reasoningTracesFromRows(rows, identityID, 1)
	if err != nil || len(traces) == 0 {
		return ReasoningTrace{}, false, err
	}
	trace := traces[0]
	stepRows, err := c.Query(ctx, getReasoningStepsStatement, params)
	if err != nil {
		return ReasoningTrace{}, false, fmt.Errorf("arcadedb: get reasoning steps: %w", err)
	}
	trace.Steps, err = reasoningStepsFromRows(stepRows, identityID, traceID)
	if err != nil {
		return ReasoningTrace{}, false, err
	}
	toolRows, err := c.Query(ctx, getReasoningToolsStatement, params)
	if err != nil {
		return ReasoningTrace{}, false, fmt.Errorf("arcadedb: get reasoning tools: %w", err)
	}
	if err := attachReasoningTools(&trace, toolRows); err != nil {
		return ReasoningTrace{}, false, err
	}
	trace, err = normalizeReasoningTrace(trace)
	if err != nil {
		return ReasoningTrace{}, false, fmt.Errorf("arcadedb: invalid stored reasoning trace: %w", err)
	}
	return trace, true, nil
}

func (c *Client) searchReasoningLexical(ctx context.Context, params map[string]any) ([]ReasoningTrace, error) {
	rows, err := c.Query(ctx, searchReasoningLexicalStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: search reasoning traces: %w", err)
	}
	return reasoningTracesFromRows(rows, params["identity_id"].(string), params["limit"].(int))
}

func reasoningTracesFromRows(rows []map[string]any, identityID string, limit int) ([]ReasoningTrace, error) {
	traces := make([]ReasoningTrace, 0, min(len(rows), limit))
	for _, row := range rows {
		if rowString(row, "identity_id") != identityID {
			continue
		}
		trace, err := reasoningTraceFromRow(row)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
		if len(traces) == limit {
			break
		}
	}
	return traces, nil
}

func reasoningTraceFromRow(row map[string]any) (ReasoningTrace, error) {
	createdAt, err := parseMemoryBatchTime(rowString(row, "created_at"))
	if err != nil {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: parse reasoning created_at: %w", err)
	}
	terminalAt, err := parseMemoryBatchTime(rowString(row, "terminal_at"))
	if err != nil {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: parse reasoning terminal_at: %w", err)
	}
	expiresAt, err := parseMemoryBatchTime(rowString(row, "expires_at"))
	if err != nil {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: parse reasoning expires_at: %w", err)
	}
	trace := ReasoningTrace{
		IdentityID: rowString(row, "identity_id"), TraceID: rowString(row, "trace_id"),
		SourceRef: rowString(row, "source_ref"), ConversationID: rowString(row, "conversation_id"),
		TurnSeq: int(rowInt(row, "turn_seq")), ProviderSummary: rowString(row, "provider_summary"),
		Status: ReasoningStatus(rowString(row, "status")), CreatedAt: createdAt,
		TerminalAt: terminalAt, ExpiresAt: expiresAt,
	}
	return normalizeReasoningTrace(trace)
}

func reasoningStepsFromRows(rows []map[string]any, identityID, traceID string) ([]ReasoningStep, error) {
	steps := make([]ReasoningStep, 0, len(rows))
	for _, row := range rows {
		if rowString(row, "identity_id") != identityID || rowString(row, "trace_id") != traceID {
			continue
		}
		createdAt, err := parseMemoryBatchTime(rowString(row, "created_at"))
		if err != nil {
			return nil, fmt.Errorf("arcadedb: parse reasoning step created_at: %w", err)
		}
		steps = append(steps, ReasoningStep{
			Index: int(rowInt(row, "step_index")), ProviderSummary: rowString(row, "provider_summary"),
			CreatedAt: createdAt,
		})
	}
	return steps, nil
}

func attachReasoningTools(trace *ReasoningTrace, rows []map[string]any) error {
	byIndex := make(map[int]*ReasoningStep, len(trace.Steps))
	for index := range trace.Steps {
		byIndex[trace.Steps[index].Index] = &trace.Steps[index]
	}
	for _, row := range rows {
		if rowString(row, "identity_id") != trace.IdentityID || rowString(row, "trace_id") != trace.TraceID {
			continue
		}
		step := byIndex[int(rowInt(row, "step_index"))]
		if step == nil {
			return fmt.Errorf("arcadedb: reasoning tool references an unknown step")
		}
		step.ToolCalls = append(step.ToolCalls, ReasoningToolCall{
			CallID: rowString(row, "call_id"), ToolName: rowString(row, "tool_name"),
			Status: rowString(row, "status"), DurationMillis: rowInt(row, "duration_ms"),
			ArgumentDigest: rowString(row, "argument_digest"), Observation: rowString(row, "observation"),
			ArtifactRefs: rowStrings(row, "artifact_refs"), EntityRefs: rowStrings(row, "entity_refs"),
			SourceRef: rowString(row, "source_ref"),
		})
	}
	return nil
}
