package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// memory_reembed is the operator's answer to "the embedder changed". A stored vector is
// only meaningful against the model that produced it, so swapping the embedder leaves the
// corpus in the wrong geometry — still answering, just worse, and never erroring. This is
// the one maintenance verb on the memory surface, and it is HIDDEN FROM THE MODEL by
// internal/agent/mcptools/bridge_memory.go: re-embedding is an operator decision about
// infrastructure, not a move an agent should make mid-turn.

// MemoryReembedInput selects the scope of the pass. The calling identity is NOT
// a field here (D-108): it travels in _meta.aura.user_identifier.
type MemoryReembedInput struct {
	// All is the difference between healing a gap and redoing the work. Default false so
	// the cheap, safe pass is what an unqualified call gets.
	All   bool `json:"all,omitempty" jsonschema:"recompute EVERY fact's vector, not only the facts that have none; use after the embedding model changes"`
	Batch int  `json:"batch,omitempty" jsonschema:"how many facts to process in one pass; defaults to 100"`
}

// MemoryReembedOutput reports what changed.
type MemoryReembedOutput struct {
	Embedded int  `json:"embedded" jsonschema:"how many facts had a vector written"`
	All      bool `json:"all" jsonschema:"whether this pass recomputed existing vectors too"`
}

func addMemoryReembedTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_reembed",
		Title: "Recompute fact vectors",
		Description: "Write embedding vectors for this identity's facts. Without `all` it " +
			"only fills facts that have none — the gap left by an embedder that was down. " +
			"With `all` it recomputes every vector, which is what an embedding-model change " +
			"requires: vectors written by a different model are in a different geometry.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false},
	}, memoryReembedHandler(tenants))
}

func memoryReembedHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[MemoryReembedInput, MemoryReembedOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		in MemoryReembedInput,
	) (*mcp.CallToolResult, MemoryReembedOutput, error) {
		_, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, MemoryReembedOutput{}, err
		}
		embed := client.EmbedMissingFacts
		if in.All {
			embed = client.ReEmbedAllFacts
		}
		written, err := embed(ctx, in.Batch)
		if err != nil {
			return nil, MemoryReembedOutput{}, fmt.Errorf("memory_reembed: %w", err)
		}
		return nil, MemoryReembedOutput{Embedded: written, All: in.All}, nil
	}
}
