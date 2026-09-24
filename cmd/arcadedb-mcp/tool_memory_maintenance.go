package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// memory_reembed is the one maintenance verb on the memory surface: a repair of fact
// vectors in the embedding space THIS server uses. It is not the answer to a model or route
// change: the daemon's scheduled pass re-embeds all memory into the new space on its own
// (memory_embed_pass.go), and this server keeps its boot route until it restarts, so an
// `all` call in that window clears every vector and re-embeds it in the OLD space.
//
// The model reaches it too: since 2026-09-03 every memory tool is bridged, deferred behind
// tool_search (internal/agent/mcptools/bridge_deferral.go). The text below is therefore what
// keeps a caller from reading it as the thing to do after a model change.

// MemoryReembedInput selects the scope of the pass. The calling identity comes
// from the authenticated OAuth subject, never a model-visible field.
type MemoryReembedInput struct {
	// All is the difference between healing a gap and redoing the work. Default false so
	// the cheap, safe pass is what an unqualified call gets.
	All   bool `json:"all,omitempty" jsonschema:"clear and recompute EVERY fact vector in this server's current space: a repair for a model file replaced under an unchanged name. A model or route change needs no call; the daemon's pass re-embeds on its own"`
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
		Description: "Repair this identity's fact vectors in the embedding space this server " +
			"uses. Without `all` it embeds one bounded page of facts outside that space. With " +
			"`all` it clears and recomputes every fact vector in that same space. A model or " +
			"route change needs no call: the daemon's scheduled pass re-embeds all memory into " +
			"the new space, and calling this before this server has restarted on the new route " +
			"would re-embed into the old one.",
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
