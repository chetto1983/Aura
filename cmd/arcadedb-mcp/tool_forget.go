package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// MemoryForgetInput names what to remove. It is a filter rather than an id
// because the thing most often worth removing is not one fact but everything a
// run wrote -- a probe, a bad extraction, a batch written under the wrong
// identity. At least one of source, entity or subject is required: an
// empty filter is refused, not read as "all".
type MemoryForgetInput struct {
	UserIdentifier string            `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
	Source         *MemoryFactSource `json:"source,omitempty" jsonschema:"detach support from this source run; facts with other sources remain"`
	Entity         string            `json:"entity,omitempty" jsonschema:"remove every fact touching this entity, in either direction, and the entity itself"`
	Subject        string            `json:"subject,omitempty" jsonschema:"remove facts with this subject"`
	Predicate      string            `json:"predicate,omitempty" jsonschema:"narrow to one relation; cannot be used alone"`
	Object         string            `json:"object,omitempty" jsonschema:"narrow to one object; cannot be used alone"`
	DryRun         bool              `json:"dry_run,omitempty" jsonschema:"count what would be removed without removing it"`
	// KeepOrphanEntities is negative because pruning is the useful default: an
	// entity whose last fact just went is a name with nothing attached to it.
	KeepOrphanEntities bool `json:"keep_orphan_entities,omitempty" jsonschema:"keep entities this removal strips of their last fact"`
}

// MemoryForgetOutput reports what went, or what would have.
type MemoryForgetOutput struct {
	Facts    int  `json:"facts" jsonschema:"facts affected by source detachment or removal"`
	Entities int  `json:"entities" jsonschema:"entities removed for having no facts left"`
	DryRun   bool `json:"dry_run,omitempty" jsonschema:"true when nothing was actually removed"`
}

func addMemoryForgetTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_forget",
		Title: "Forget facts",
		Description: "Detach a source's support or remove matching facts and orphaned entities. " +
			"A source-scoped call keeps facts that still have another source. Use this for " +
			"what was never true -- a probe, a bad extraction, a run written under the wrong " +
			"identity. For something that STOPPED being true, use memory_upsert_fact with " +
			"supersedes instead: that closes the fact's window and keeps the past answerable, " +
			"where this destroys it. Pass dry_run first to see the size of what you are about " +
			"to remove.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: new(false),
			IdempotentHint:  true,
		},
	}, memoryForgetHandler(tenants))
}

func memoryForgetHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[MemoryForgetInput, MemoryForgetOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryForgetInput,
	) (*mcp.CallToolResult, MemoryForgetOutput, error) {
		client, err := tenants.For(ctx, in.UserIdentifier)
		if err != nil {
			return nil, MemoryForgetOutput{}, err
		}
		sourceRunID := ""
		if in.Source != nil {
			sourceRunID = in.Source.RunID
		}
		result, err := client.Forget(ctx, arcadedb.ForgetFilter{
			SourceRunID: sourceRunID,
			Entity:      in.Entity,
			Subject:     in.Subject,
			Predicate:   in.Predicate,
			Object:      in.Object,
			DryRun:      in.DryRun,
			KeepOrphans: in.KeepOrphanEntities,
		})
		if err != nil {
			return nil, MemoryForgetOutput{}, fmt.Errorf("memory_forget: %w", err)
		}
		return nil, MemoryForgetOutput{
			Facts:    result.Facts,
			Entities: result.Entities,
			DryRun:   result.DryRun,
		}, nil
	}
}
