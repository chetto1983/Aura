package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const (
	// Defined independently from bridge_recall_context.go because this binary and
	// the agent bridge share a loopback wire contract, not a Go package boundary.
	// The literal and codec bounds must stay byte-identical on both sides.
	memoryRecallActiveSourcesHeader = "X-Aura-Active-Sources"
	memoryRecallContextVersion      = 1
	memoryRecallMaxActiveSources    = 8
	memoryRecallMaxActiveIDRunes    = 256
	memoryRecallMaxDecodedHeader    = 1536
	memoryRecallMaxEncodedHeader    = 2048
)

type memoryRecallActiveSource struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
}

type memoryRecallActiveEnvelope struct {
	Version int                        `json:"version"`
	Sources []memoryRecallActiveSource `json:"sources"`
}

// memoryRecallActiveOwnershipStatement reads the conversations a caller wants excluded,
// WITHOUT filtering on the identity — the comparison happens in Go so a row belonging to
// someone else can be seen and refused. Filtering here instead made the check inert: a
// mis-tenanted row and a conversation that does not exist yet both came back as zero rows,
// indistinguishable, and the code treated both as a violation.
const memoryRecallActiveOwnershipStatement = "SELECT identity_id, conversation_id FROM Conversation" +
	" WHERE conversation_id IN :conversation_ids"

// MemoryRecallInput is the additive public contract for the single memory read.
type MemoryRecallInput struct {
	Mode           string `json:"mode,omitempty" jsonschema:"semantic (default), recent, open, scroll, or reserved reasoning"`
	Query          string `json:"query,omitempty" jsonschema:"what to recall in natural language; used by semantic mode"`
	Entity         string `json:"entity,omitempty" jsonschema:"exact entity name when known; selects graph traversal in semantic mode"`
	Predicate      string `json:"predicate,omitempty" jsonschema:"optional relation filter used with entity"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"stable conversation id used by open and scroll"`
	AnchorSeq      int    `json:"anchor_seq,omitempty" jsonschema:"stable turn sequence used by open and scroll"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by an earlier open or scroll call"`
	Direction      string `json:"direction,omitempty" jsonschema:"before or after the stable anchor"`
	TraceID        string `json:"trace_id,omitempty" jsonschema:"exact reasoning trace id; used only by explicit reasoning mode"`
	Limit          int    `json:"limit,omitempty" jsonschema:"bounded number of evidence records or conversation turns"`
	AsOf           string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; return facts valid then rather than now"`
}

// MemoryConversationTurn is one authoritative projected turn in a bounded window.
type MemoryConversationTurn struct {
	Seq        int    `json:"seq"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at"`
	SourceRef  string `json:"source_ref"`
}

// MemoryConversationEvidence is a bounded historical span around one stable anchor.
type MemoryConversationEvidence struct {
	ConversationID string                   `json:"conversation_id"`
	AnchorSeq      int                      `json:"anchor_seq"`
	Turns          []MemoryConversationTurn `json:"turns"`
}

// MemoryRecallEvidence is a typed fact-or-conversation union.
type MemoryRecallEvidence struct {
	Kind         string                      `json:"kind"`
	Rank         int                         `json:"rank"`
	Score        float64                     `json:"score,omitempty"`
	Fact         *MemorySearchHit            `json:"fact,omitempty"`
	Conversation *MemoryConversationEvidence `json:"conversation,omitempty"`
	Reasoning    *MemoryReasoningTrace       `json:"reasoning,omitempty"`
}

// MemoryReasoningToolCall is bounded redacted evidence for one invocation.
type MemoryReasoningToolCall struct {
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

// MemoryReasoningStep is one ordered provider-visible part of a trace.
type MemoryReasoningStep struct {
	Index           int                       `json:"index"`
	ProviderSummary string                    `json:"provider_summary"`
	CreatedAt       string                    `json:"created_at"`
	ToolCalls       []MemoryReasoningToolCall `json:"tool_calls,omitempty"`
}

// MemoryReasoningTrace is an explicit-only provider-visible reasoning graph.
type MemoryReasoningTrace struct {
	TraceID         string                `json:"trace_id"`
	SourceRef       string                `json:"source_ref"`
	ConversationID  string                `json:"conversation_id"`
	TurnSeq         int                   `json:"turn_seq"`
	ProviderSummary string                `json:"provider_summary"`
	Status          string                `json:"status"`
	CreatedAt       string                `json:"created_at"`
	TerminalAt      string                `json:"terminal_at,omitempty"`
	ExpiresAt       string                `json:"expires_at,omitempty"`
	Steps           []MemoryReasoningStep `json:"steps,omitempty"`
}

// MemoryRecallRetrievalMetadata keeps evidence contribution distinct from backend execution.
type MemoryRecallRetrievalMetadata struct {
	EffectivePath     string `json:"effective_path"`
	Path              string `json:"path"`
	FactCount         int    `json:"fact_count"`
	ConversationCount int    `json:"conversation_count"`
	ReasoningCount    int    `json:"reasoning_count"`
	Abstained         bool   `json:"abstained"`
	Reason            string `json:"reason,omitempty"`
}

// MemoryRecallOutput is additive: Facts preserves the shipped fact-only projection.
type MemoryRecallOutput struct {
	Evidence   []MemoryRecallEvidence        `json:"evidence"`
	Facts      []MemorySearchHit             `json:"facts"`
	Abstained  bool                          `json:"abstained"`
	Reason     string                        `json:"reason,omitempty"`
	NextCursor string                        `json:"next_cursor,omitempty"`
	Retrieval  MemoryRecallRetrievalMetadata `json:"retrieval"`
}

func addMemoryRecallTool(server *mcp.Server, tenants *tenants) {
	inputSchema, err := jsonschema.For[MemoryRecallInput](nil)
	if err != nil {
		panic(fmt.Sprintf("memory_recall input schema: %v", err))
	}
	inputSchema.Properties["mode"].Enum = []any{
		"semantic", "recent", "open", "scroll", "reasoning",
	}
	inputSchema.Properties["direction"].Enum = []any{"before", "after"}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_recall",
		Title:       "Recall memory",
		InputSchema: inputSchema,
		Description: "The single deep-read operation for Aura memory. Choose the mode from the question: " +
			"use recent when it asks what happened before rather than about a topic -- what you two have " +
			"discussed, what you remember of the user, anything that means look back; the default semantic " +
			"mode matches wording and will miss those, answering that it recalls nothing. Use semantic with " +
			"query for a specific topic, or entity for a name you already know. Use open and scroll with " +
			"conversation_id to page through one conversation's turns. Reasoning explicitly searches or opens " +
			"provider-visible traces and is never included by other modes. Weak evidence abstains explicitly.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memoryRecallHandler(tenants))
}

func memoryRecallHandler(tenants *tenants) mcp.ToolHandlerFor[MemoryRecallInput, MemoryRecallOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		in MemoryRecallInput,
	) (*mcp.CallToolResult, MemoryRecallOutput, error) {
		identity, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, MemoryRecallOutput{}, err
		}
		if arcadedb.RecallMode(in.Mode) == arcadedb.RecallModeReasoning {
			output, err := memoryReasoningRecall(ctx, client, identity, in)
			if err != nil {
				return nil, MemoryRecallOutput{}, fmt.Errorf("memory_recall: %w", err)
			}
			return nil, output, nil
		}
		excludedConversations, err := memoryRecallActiveConversationIDs(ctx, req, identity, client)
		if err != nil {
			return nil, MemoryRecallOutput{}, err
		}
		_ = excludedConversations
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemoryRecallOutput{}, err
		}
		result, err := client.RecallMemory(ctx, arcadedb.RecallRequest{
			IdentityID: identity, Mode: arcadedb.RecallMode(in.Mode), Query: in.Query,
			Entity: in.Entity, Predicate: in.Predicate, AsOf: asOf,
			ConversationID: in.ConversationID, AnchorSeq: in.AnchorSeq,
			Cursor: in.Cursor, Direction: arcadedb.RecallDirection(in.Direction), Limit: in.Limit,
			ExcludeConversationIDs: excludedConversations,
		})
		if err != nil {
			return nil, MemoryRecallOutput{}, fmt.Errorf("memory_recall: %w", err)
		}
		recordMemoryRecallTelemetry(ctx, in.Mode, result)
		return nil, memoryRecallOutput(result), nil
	}
}

func memoryReasoningRecall(
	ctx context.Context,
	client *arcadedb.Client,
	identity string,
	in MemoryRecallInput,
) (MemoryRecallOutput, error) {
	query, traceID := strings.TrimSpace(in.Query), strings.TrimSpace(in.TraceID)
	if (query == "") == (traceID == "") {
		return MemoryRecallOutput{}, fmt.Errorf("reasoning mode requires exactly one of query or trace_id")
	}
	if in.Entity != "" || in.Predicate != "" || in.ConversationID != "" || in.AnchorSeq != 0 ||
		in.Cursor != "" || in.Direction != "" || in.AsOf != "" {
		return MemoryRecallOutput{}, fmt.Errorf("reasoning mode rejects ordinary recall selectors")
	}
	var traces []arcadedb.ReasoningTrace
	if traceID != "" {
		trace, found, err := client.GetReasoningTrace(ctx, identity, traceID)
		if err != nil {
			return MemoryRecallOutput{}, err
		}
		if found {
			traces = []arcadedb.ReasoningTrace{trace}
		}
	} else {
		var err error
		traces, err = client.SearchReasoningTraces(ctx, identity, query, in.Limit)
		if err != nil {
			return MemoryRecallOutput{}, err
		}
	}
	output := MemoryRecallOutput{
		Evidence: make([]MemoryRecallEvidence, 0, len(traces)), Facts: make([]MemorySearchHit, 0),
		Abstained: len(traces) == 0,
		Retrieval: MemoryRecallRetrievalMetadata{
			EffectivePath: "reasoning", Path: "graph", ReasoningCount: len(traces),
			Abstained: len(traces) == 0,
		},
	}
	if output.Abstained {
		output.Reason = "no_reasoning_evidence"
		output.Retrieval.Reason = output.Reason
	}
	for index, trace := range traces {
		converted := memoryReasoningTrace(trace)
		output.Evidence = append(output.Evidence, MemoryRecallEvidence{
			Kind: "reasoning", Rank: index + 1, Reasoning: &converted,
		})
	}
	recordMemoryRecallTelemetry(ctx, in.Mode, arcadedb.RecallResult{
		Abstained: output.Abstained, Reason: output.Reason,
		Retrieval: arcadedb.RecallRetrieval{
			EffectivePath: "reasoning", Path: "graph", ReasoningCount: len(traces),
		},
	})
	return output, nil
}

func memoryReasoningTrace(trace arcadedb.ReasoningTrace) MemoryReasoningTrace {
	output := MemoryReasoningTrace{
		TraceID: trace.TraceID, SourceRef: trace.SourceRef, ConversationID: trace.ConversationID,
		TurnSeq: trace.TurnSeq, ProviderSummary: trace.ProviderSummary, Status: string(trace.Status),
		CreatedAt: trace.CreatedAt.UTC().Format(time.RFC3339Nano),
		Steps:     make([]MemoryReasoningStep, 0, len(trace.Steps)),
	}
	if !trace.TerminalAt.IsZero() {
		output.TerminalAt = trace.TerminalAt.UTC().Format(time.RFC3339Nano)
	}
	if !trace.ExpiresAt.IsZero() {
		output.ExpiresAt = trace.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	for _, step := range trace.Steps {
		converted := MemoryReasoningStep{
			Index: step.Index, ProviderSummary: step.ProviderSummary,
			CreatedAt: step.CreatedAt.UTC().Format(time.RFC3339Nano),
			ToolCalls: make([]MemoryReasoningToolCall, 0, len(step.ToolCalls)),
		}
		for _, tool := range step.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, MemoryReasoningToolCall{
				CallID: tool.CallID, ToolName: tool.ToolName, Status: tool.Status,
				DurationMillis: tool.DurationMillis, ArgumentDigest: tool.ArgumentDigest,
				Observation: tool.Observation, ArtifactRefs: tool.ArtifactRefs,
				EntityRefs: tool.EntityRefs, SourceRef: tool.SourceRef,
			})
		}
		output.Steps = append(output.Steps, converted)
	}
	return output
}

// memoryRecallActiveConversationIDs decodes and revalidates host-only recall exclusions
// after OAuth identity resolution. The turn id must equal the separately carried host
// actor run id.
//
// A conversation the graph does not hold yet is NOT a violation. These ids become one
// thing only — ExcludeConversationIDs, so the turn being taken is not recalled into itself
// — and excluding a conversation with no projected turns excludes nothing. Requiring every
// id to resolve made the FIRST recall of every new conversation fail: the projection is
// asynchronous, so seconds after a conversation begins its vertex does not exist, and the
// caller got "not owned by the authenticated identity", which reads as a security failure
// rather than as a race. Measured live on 2026-09-03, two turns after a conversation
// opened.
//
// What IS refused is a row that exists and belongs to someone else. Memory is one database
// per identity, so that cannot happen by construction — which is exactly why it is worth
// reporting if it ever does, rather than being filtered out of the query before anyone can
// see it.
func memoryRecallActiveConversationIDs(
	ctx context.Context,
	req *mcp.CallToolRequest,
	identity string,
	client *arcadedb.Client,
) ([]string, error) {
	if req == nil || req.Extra == nil {
		return nil, nil
	}
	values := req.Extra.Header.Values(memoryRecallActiveSourcesHeader)
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("memory_recall: active-source header must appear exactly once")
	}
	sources, err := decodeMemoryRecallActiveSources(values[0])
	if err != nil {
		return nil, fmt.Errorf("memory_recall: %w", err)
	}
	actorTurnID := strings.TrimSpace(req.Extra.Header.Get(memoryActorRunIDHeader))
	if actorTurnID == "" {
		return nil, fmt.Errorf("memory_recall: active-source header requires the host actor run id")
	}
	conversationSet := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.TurnID != actorTurnID {
			return nil, fmt.Errorf("memory_recall: active-source turn does not match the host actor")
		}
		conversationSet[source.ConversationID] = struct{}{}
	}
	conversationIDs := make([]string, 0, len(conversationSet))
	for conversationID := range conversationSet {
		conversationIDs = append(conversationIDs, conversationID)
	}
	sort.Strings(conversationIDs)
	rows, err := client.Query(ctx, memoryRecallActiveOwnershipStatement, map[string]any{
		"conversation_ids": conversationIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("memory_recall: revalidate active-source ownership: %w", err)
	}
	owned := make([]string, 0, len(rows))
	for _, row := range rows {
		conversationID, _ := row["conversation_id"].(string)
		if _, requested := conversationSet[conversationID]; !requested {
			continue
		}
		if rowIdentity, _ := row["identity_id"].(string); rowIdentity != identity {
			return nil, fmt.Errorf("memory_recall: active-source conversation is not owned by the authenticated identity")
		}
		owned = append(owned, conversationID)
	}
	sort.Strings(owned)
	return slices.Compact(owned), nil
}

func decodeMemoryRecallActiveSources(encoded string) ([]memoryRecallActiveSource, error) {
	if encoded == "" || encoded != strings.TrimSpace(encoded) {
		return nil, fmt.Errorf("active-source header is empty or non-canonical")
	}
	if len(encoded) > memoryRecallMaxEncodedHeader {
		return nil, fmt.Errorf("active-source header exceeds %d bytes", memoryRecallMaxEncodedHeader)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid active-source encoding: %w", err)
	}
	if len(raw) > memoryRecallMaxDecodedHeader {
		return nil, fmt.Errorf("active-source payload exceeds %d bytes", memoryRecallMaxDecodedHeader)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope memoryRecallActiveEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("invalid active-source payload: %w", err)
	}
	if err := ensureMemoryRecallActiveEOF(decoder); err != nil {
		return nil, err
	}
	if envelope.Version != memoryRecallContextVersion {
		return nil, fmt.Errorf("active-source version %d is unsupported", envelope.Version)
	}
	canonical, err := encodeMemoryRecallActiveSources(envelope.Sources)
	if err != nil {
		return nil, err
	}
	if canonical != encoded {
		return nil, fmt.Errorf("active-source header is not canonical")
	}
	return envelope.Sources, nil
}

func encodeMemoryRecallActiveSources(sources []memoryRecallActiveSource) (string, error) {
	if len(sources) == 0 || len(sources) > memoryRecallMaxActiveSources {
		return "", fmt.Errorf("active-source payload must contain between 1 and %d entries", memoryRecallMaxActiveSources)
	}
	canonical := append([]memoryRecallActiveSource(nil), sources...)
	for _, source := range canonical {
		if err := validateMemoryRecallActiveSource(source); err != nil {
			return "", err
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].ConversationID != canonical[j].ConversationID {
			return canonical[i].ConversationID < canonical[j].ConversationID
		}
		return canonical[i].TurnID < canonical[j].TurnID
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index] == canonical[index-1] {
			return "", fmt.Errorf("active-source payload contains a duplicate")
		}
	}
	raw, err := json.Marshal(memoryRecallActiveEnvelope{Version: memoryRecallContextVersion, Sources: canonical})
	if err != nil {
		return "", fmt.Errorf("encode active-source payload: %w", err)
	}
	if len(raw) > memoryRecallMaxDecodedHeader {
		return "", fmt.Errorf("active-source payload exceeds %d bytes", memoryRecallMaxDecodedHeader)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > memoryRecallMaxEncodedHeader {
		return "", fmt.Errorf("active-source header exceeds %d bytes", memoryRecallMaxEncodedHeader)
	}
	return encoded, nil
}

func validateMemoryRecallActiveSource(source memoryRecallActiveSource) error {
	for name, value := range map[string]string{
		"conversation_id": source.ConversationID,
		"turn_id":         source.TurnID,
	} {
		if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > memoryRecallMaxActiveIDRunes {
			return fmt.Errorf("active-source %s is empty, non-canonical, or over %d runes", name, memoryRecallMaxActiveIDRunes)
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return fmt.Errorf("active-source %s contains a control character", name)
			}
		}
	}
	return nil
}

func ensureMemoryRecallActiveEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("active-source payload has trailing JSON")
		}
		return fmt.Errorf("invalid active-source payload: %w", err)
	}
	return nil
}

func memoryRecallOutput(result arcadedb.RecallResult) MemoryRecallOutput {
	output := MemoryRecallOutput{
		Evidence:  make([]MemoryRecallEvidence, 0, len(result.Evidence)),
		Facts:     make([]MemorySearchHit, 0, result.Retrieval.FactCount),
		Abstained: result.Abstained, Reason: result.Reason, NextCursor: result.NextCursor,
		Retrieval: MemoryRecallRetrievalMetadata{
			EffectivePath: result.Retrieval.EffectivePath, Path: result.Retrieval.Path,
			FactCount: result.Retrieval.FactCount, ConversationCount: result.Retrieval.ConversationCount,
			ReasoningCount: result.Retrieval.ReasoningCount,
			Abstained:      result.Abstained, Reason: result.Reason,
		},
	}
	for _, evidence := range result.Evidence {
		item := MemoryRecallEvidence{
			Kind: string(evidence.Kind), Rank: evidence.Rank, Score: evidence.Score,
		}
		if evidence.Fact != nil {
			facts := toHits([]arcadedb.FactHit{*evidence.Fact})
			if len(facts) == 1 {
				fact := facts[0]
				item.Fact = &fact
				output.Facts = append(output.Facts, fact)
			}
		}
		if evidence.Conversation != nil {
			conversation := MemoryConversationEvidence{
				ConversationID: evidence.Conversation.ConversationID,
				AnchorSeq:      evidence.Conversation.AnchorSeq,
				Turns:          make([]MemoryConversationTurn, 0, len(evidence.Conversation.Turns)),
			}
			for _, turn := range evidence.Conversation.Turns {
				conversation.Turns = append(conversation.Turns, MemoryConversationTurn{
					Seq: turn.Seq, Role: turn.Role, Content: turn.Content,
					OccurredAt: turn.OccurredAt, SourceRef: turn.SourceRef,
				})
			}
			item.Conversation = &conversation
		}
		output.Evidence = append(output.Evidence, item)
	}
	return output
}

func recordMemoryRecallTelemetry(ctx context.Context, mode string, result arcadedb.RecallResult) {
	if mode == "" {
		mode = string(arcadedb.RecallModeSemantic)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("memory.recall.mode", mode),
		attribute.String("memory.recall.effective_path", result.Retrieval.EffectivePath),
		attribute.String("memory.recall.path", result.Retrieval.Path),
		attribute.Int("memory.recall.fact_candidates", result.Retrieval.FactCandidateCount),
		attribute.Int("memory.recall.conversation_candidates", result.Retrieval.ConversationCandidates),
		attribute.Int("memory.recall.fact_count", result.Retrieval.FactCount),
		attribute.Int("memory.recall.conversation_count", result.Retrieval.ConversationCount),
		attribute.Int("memory.recall.reasoning_count", result.Retrieval.ReasoningCount),
		attribute.String("memory.recall.abstention_reason", result.Reason),
		attribute.Float64("memory.recall.backend_latency_ms", float64(result.Retrieval.BackendLatency.Microseconds())/1000),
	)
}
