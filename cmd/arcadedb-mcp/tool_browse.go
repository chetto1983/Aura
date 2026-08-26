package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// The browsing half. Search answers a question; these answer "what is there",
// which is the question you have when you do not yet know enough to ask one.
// Without them an agent holding this surface is blind: memory_facts_about needs
// an exact name, memory_search needs the right words, and graph_schema returns
// counts rather than names.

// MemoryEntitiesInput bounds the listing. The calling identity comes from the
// authenticated OAuth subject, never a model-visible field.
type MemoryEntitiesInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many entities to return; defaults to 50"`
}

// MemoryEntitiesOutput carries the names the memory knows.
type MemoryEntitiesOutput struct {
	Entities []arcadedb.EntitySummary `json:"entities"`
}

func addMemoryEntitiesTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_entities",
		Title: "List entities",
		Description: "List the entities the memory knows, the ones holding the most facts " +
			"first. Use it to find the exact name memory_facts_about needs.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		in MemoryEntitiesInput,
	) (*mcp.CallToolResult, MemoryEntitiesOutput, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, MemoryEntitiesOutput{}, err
		}
		entities, err := client.ListEntities(ctx, in.Limit)
		if err != nil {
			return nil, MemoryEntitiesOutput{}, fmt.Errorf("memory_entities: %w", err)
		}
		return nil, MemoryEntitiesOutput{Entities: entities}, nil
	})
}

// MemoryDigestInput shapes the index. The calling identity comes from the
// authenticated OAuth subject, never a model-visible field.
type MemoryDigestInput struct {
	Limit          int `json:"limit,omitempty" jsonschema:"how many entities to include; defaults to 50"`
	FactsPerEntity int `json:"facts_per_entity,omitempty" jsonschema:"how many facts to show per entity; defaults to 3"`
}

func addMemoryDigestTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_digest",
		Title: "Memory index",
		Description: "Return the whole memory as a compact index, one line per entity, meant " +
			"to be READ ONCE AND KEPT rather than queried repeatedly. While it fits in " +
			"context, searching is unnecessary: measured on a real 158-note memory, " +
			"full-text search found the right note at rank 1 in 6 cases out of 10 and the " +
			"index in 10 out of 10. Check `covered`: when it is false the memory is larger " +
			"than the index and memory_search is still needed for what was left out.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		in MemoryDigestInput,
	) (*mcp.CallToolResult, arcadedb.DigestResult, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, arcadedb.DigestResult{}, err
		}
		digest, err := client.Digest(ctx, in.Limit, in.FactsPerEntity)
		if err != nil {
			return nil, arcadedb.DigestResult{}, fmt.Errorf("memory_digest: %w", err)
		}
		return nil, digest, nil
	})
}

// MemoryMergeInput names the duplicate and the survivor. The calling identity
// comes from the authenticated OAuth subject, never a model-visible field.
type MemoryMergeInput struct {
	Source string `json:"source" jsonschema:"the duplicate name; it is removed"`
	Target string `json:"target" jsonschema:"the name that survives and receives the facts"`
}

func addMemoryMergeTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_merge_entities",
		Title: "Merge two entities",
		Description: "Fold one entity's facts onto another and remove it, for when the same " +
			"thing was recorded under two names. Prefer this over memory_forget for a " +
			"duplicate: forgetting destroys the facts only the duplicate knew. If the " +
			"target does not exist yet this is a rename.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: new(true),
			IdempotentHint:  true,
		},
	}, func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		in MemoryMergeInput,
	) (*mcp.CallToolResult, arcadedb.MergeResult, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, arcadedb.MergeResult{}, err
		}
		result, err := client.MergeEntities(ctx, in.Source, in.Target)
		if err != nil {
			return nil, arcadedb.MergeResult{}, fmt.Errorf("memory_merge_entities: %w", err)
		}
		return nil, result, nil
	})
}
