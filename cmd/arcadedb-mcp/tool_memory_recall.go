package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

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
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_recall",
		Title: "Recall memory",
		Description: "The single deep-read operation for Aura memory. Semantic mode (the default) " +
			"can return typed fact and historical-conversation evidence together. Recent, open, and " +
			"scroll progressively browse bounded conversation windows; reasoning is reserved until " +
			"the explicit reasoning contract ships. Weak evidence abstains explicitly.",
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
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemoryRecallOutput{}, err
		}
		result, err := client.RecallMemory(ctx, arcadedb.RecallRequest{
			IdentityID: identity, Mode: arcadedb.RecallMode(in.Mode), Query: in.Query,
			Entity: in.Entity, Predicate: in.Predicate, AsOf: asOf,
			ConversationID: in.ConversationID, AnchorSeq: in.AnchorSeq,
			Cursor: in.Cursor, Direction: arcadedb.RecallDirection(in.Direction), Limit: in.Limit,
		})
		if err != nil {
			return nil, MemoryRecallOutput{}, fmt.Errorf("memory_recall: %w", err)
		}
		return nil, memoryRecallOutput(result), nil
	}
}

func memoryRecallOutput(result arcadedb.RecallResult) MemoryRecallOutput {
	return MemoryRecallOutput{
		Evidence: make([]MemoryRecallEvidence, 0), Facts: make([]MemorySearchHit, 0),
		Abstained: result.Abstained, Reason: result.Reason, NextCursor: result.NextCursor,
		Retrieval: MemoryRecallRetrievalMetadata{
			EffectivePath: result.Retrieval.EffectivePath, Path: result.Retrieval.Path,
			FactCount: result.Retrieval.FactCount, ConversationCount: result.Retrieval.ConversationCount,
			ReasoningCount: result.Retrieval.ReasoningCount,
			Abstained:      result.Abstained, Reason: result.Reason,
		},
	}
}
