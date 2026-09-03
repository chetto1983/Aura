package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const (
	memoryBatchMaxOperations       = 100
	memoryBatchIdempotencyMaxRunes = 100
	memoryBatchEntityMaxRunes      = 512
	memoryBatchPredicateMaxRunes   = 100
	memoryBatchStatementMaxRunes   = 4096
	memoryBatchMemoryIDMaxRunes    = 100
	memoryBatchMemoryIDsMaxCount   = 64
)

// MemoryBatchFactInput carries one bounded fact mutation without host authority fields.
type MemoryBatchFactInput struct {
	Subject     string `json:"subject" jsonschema:"the entity the fact is about"`
	SubjectKind string `json:"subject_kind,omitempty" jsonschema:"optional entity kind"`
	// SubjectPole and ObjectPole are here because memory_upsert_fact has them and
	// this is the same write. Their absence was not a smaller surface, it was a
	// worse one: batch is the BULK path, the one an import agent uses to coin many
	// entities at once, so the caller most likely to need the class was the only one
	// unable to state it. Every entity it wrote fell to whatever poleByKind could
	// derive, and to Other when it could derive nothing.
	SubjectPole       string                      `json:"subject_pole,omitempty" jsonschema:"POLE class: Person, Object, Location, Event, Organisation or Other. Omit to derive it from subject_kind; anything outside the set lands in Other and is reported back"`
	Predicate         string                      `json:"predicate" jsonschema:"the relation between subject and object"`
	Object            string                      `json:"object" jsonschema:"the related entity or scalar value"`
	ObjectKind        string                      `json:"object_kind,omitempty" jsonschema:"optional object entity kind"`
	ObjectPole        string                      `json:"object_pole,omitempty" jsonschema:"POLE class: Person, Object, Location, Event, Organisation or Other. Omit to derive it from object_kind"`
	Statement         string                      `json:"statement" jsonschema:"the fact in natural language"`
	ValidFrom         string                      `json:"valid_from,omitempty" jsonschema:"RFC3339 instant when the fact became true"`
	ValidTo           string                      `json:"valid_to,omitempty" jsonschema:"RFC3339 instant when the fact stopped being true"`
	SupersedesFactKey string                      `json:"supersedes_fact_key,omitempty" jsonschema:"exact fact key to close for supersede_fact"`
	Source            MemoryUpsertFactWriteSource `json:"source" jsonschema:"direct supporting memory ids; actor provenance is host-derived"`
}

// MemoryBatchMergeInput names an entity folded into a surviving target.
type MemoryBatchMergeInput struct {
	Source string `json:"source" jsonschema:"entity name to fold away"`
	Target string `json:"target" jsonschema:"surviving entity name"`
}

// MemoryBatchForgetInput names destructive fact evidence to remove.
type MemoryBatchForgetInput struct {
	SourceRunID        string `json:"source_run_id,omitempty" jsonschema:"detach evidence written by this existing source run"`
	Entity             string `json:"entity,omitempty" jsonschema:"remove facts touching this entity"`
	Subject            string `json:"subject,omitempty" jsonschema:"remove facts with this subject"`
	Predicate          string `json:"predicate,omitempty" jsonschema:"optional relation filter"`
	Object             string `json:"object,omitempty" jsonschema:"optional object filter"`
	KeepOrphanEntities bool   `json:"keep_orphan_entities,omitempty" jsonschema:"retain entities left without facts"`
}

// MemoryBatchOperationInput is the model-facing four-variant tagged union.
type MemoryBatchOperationInput struct {
	Type   string                  `json:"type" jsonschema:"upsert_fact, supersede_fact, merge_entities, or forget"`
	Fact   *MemoryBatchFactInput   `json:"fact,omitempty" jsonschema:"payload for upsert_fact or supersede_fact"`
	Merge  *MemoryBatchMergeInput  `json:"merge,omitempty" jsonschema:"payload for merge_entities"`
	Forget *MemoryBatchForgetInput `json:"forget,omitempty" jsonschema:"payload for forget"`
}

// MemoryBatchInput carries one idempotency key and ordered operations.
type MemoryBatchInput struct {
	IdempotencyKey string                      `json:"idempotency_key" jsonschema:"stable key for replaying this exact identity-bound batch"`
	Operations     []MemoryBatchOperationInput `json:"operations" jsonschema:"ordered final-state memory mutations"`
}

// MemoryBatchOutput exposes only the committed final result.
type MemoryBatchOutput struct {
	Applied    int                                   `json:"applied"`
	Replayed   bool                                  `json:"replayed"`
	Operations []arcadedb.MemoryBatchOperationResult `json:"operations"`
}

type memoryBatchApply func(
	context.Context,
	*arcadedb.Client,
	arcadedb.MemoryBatchActor,
	arcadedb.MemoryBatchRequest,
	time.Time,
) (arcadedb.MemoryBatchResult, error)

func addMemoryBatchTool(server *mcp.Server, tenants *tenants, now clock, operatorDisplayName string) {
	inputSchema, err := jsonschema.For[MemoryBatchInput](nil)
	if err != nil {
		panic(fmt.Sprintf("memory_batch input schema: %v", err))
	}
	boundMemoryBatchSchema(inputSchema)
	destructive, openWorld := true, false
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_batch",
		Title: "Apply atomic memory changes",
		Description: "Apply ordered fact upserts, precise supersessions, entity merges, and forgetting " +
			"as one identity-scoped final-state transaction. Supply a stable idempotency key; any invalid " +
			"operation leaves live memory unchanged.",
		InputSchema: inputSchema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false, DestructiveHint: &destructive,
			IdempotentHint: true, OpenWorldHint: &openWorld,
		},
	}, memoryBatchHandler(tenants, now, operatorDisplayName, applyMemoryBatchOnce))
}

func memoryBatchHandler(
	tenants *tenants,
	now clock,
	operatorDisplayName string,
	apply memoryBatchApply,
) mcp.ToolHandlerFor[MemoryBatchInput, MemoryBatchOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in MemoryBatchInput) (*mcp.CallToolResult, MemoryBatchOutput, error) {
		identity, client, err := resolveCaller(ctx, tenants, req)
		if err != nil {
			return nil, MemoryBatchOutput{}, err
		}
		actor, err := hostDerivedActor(req)
		if err != nil {
			return nil, MemoryBatchOutput{}, err
		}
		request, err := toMemoryBatchRequest(in, actor, identity, operatorDisplayName)
		if err != nil {
			return nil, MemoryBatchOutput{}, err
		}
		result, err := apply(ctx, client, arcadedb.MemoryBatchActor{IdentityID: identity, WriterRole: actor.Role}, request, now())
		if err != nil {
			return nil, MemoryBatchOutput{}, err
		}
		return nil, MemoryBatchOutput{Applied: result.Applied, Replayed: result.Replayed, Operations: result.Operations}, nil
	}
}

func applyMemoryBatchOnce(
	ctx context.Context,
	client *arcadedb.Client,
	actor arcadedb.MemoryBatchActor,
	request arcadedb.MemoryBatchRequest,
	now time.Time,
) (arcadedb.MemoryBatchResult, error) {
	return client.ApplyMemoryBatch(ctx, actor, request, now)
}

func toMemoryBatchRequest(
	in MemoryBatchInput,
	actor arcadedb.Actor,
	identity string,
	operatorDisplayName string,
) (arcadedb.MemoryBatchRequest, error) {
	if utf8.RuneCountInString(in.IdempotencyKey) > memoryBatchIdempotencyMaxRunes {
		return arcadedb.MemoryBatchRequest{}, fmt.Errorf("memory_batch: idempotency_key exceeds %d runes", memoryBatchIdempotencyMaxRunes)
	}
	if len(in.Operations) == 0 || len(in.Operations) > memoryBatchMaxOperations {
		return arcadedb.MemoryBatchRequest{}, fmt.Errorf("memory_batch: operations must contain between 1 and %d items", memoryBatchMaxOperations)
	}
	operations := make([]arcadedb.MemoryBatchOperation, len(in.Operations))
	for index, operation := range in.Operations {
		converted, err := toMemoryBatchOperation(operation, actor, identity, operatorDisplayName)
		if err != nil {
			return arcadedb.MemoryBatchRequest{}, fmt.Errorf("memory_batch: operation %d: %w", index, err)
		}
		operations[index] = converted
	}
	return arcadedb.MemoryBatchRequest{IdempotencyKey: in.IdempotencyKey, Operations: operations}, nil
}

func toMemoryBatchOperation(
	in MemoryBatchOperationInput,
	actor arcadedb.Actor,
	identity string,
	operatorDisplayName string,
) (arcadedb.MemoryBatchOperation, error) {
	payloads := 0
	for _, present := range []bool{in.Fact != nil, in.Merge != nil, in.Forget != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return arcadedb.MemoryBatchOperation{}, fmt.Errorf("exactly one operation payload is required")
	}
	switch arcadedb.MemoryBatchOperationType(in.Type) {
	case arcadedb.MemoryBatchUpsertFact, arcadedb.MemoryBatchSupersedeFact:
		if in.Fact == nil {
			return arcadedb.MemoryBatchOperation{}, fmt.Errorf("%s requires a fact payload", in.Type)
		}
		fact, err := toMemoryBatchFact(*in.Fact, actor, identity, operatorDisplayName)
		if err != nil {
			return arcadedb.MemoryBatchOperation{}, err
		}
		return arcadedb.MemoryBatchOperation{Type: arcadedb.MemoryBatchOperationType(in.Type), Fact: &fact}, nil
	case arcadedb.MemoryBatchMergeEntities:
		if in.Merge == nil {
			return arcadedb.MemoryBatchOperation{}, fmt.Errorf("merge_entities requires a merge payload")
		}
		return arcadedb.MemoryBatchOperation{
			Type:  arcadedb.MemoryBatchMergeEntities,
			Merge: &arcadedb.MemoryBatchMerge{Source: in.Merge.Source, Target: in.Merge.Target},
		}, nil
	case arcadedb.MemoryBatchForget:
		if in.Forget == nil {
			return arcadedb.MemoryBatchOperation{}, fmt.Errorf("forget requires a forget payload")
		}
		return arcadedb.MemoryBatchOperation{
			Type: arcadedb.MemoryBatchForget,
			Forget: &arcadedb.ForgetFilter{
				SourceRunID: in.Forget.SourceRunID, Entity: in.Forget.Entity,
				Subject: in.Forget.Subject, Predicate: in.Forget.Predicate, Object: in.Forget.Object,
				KeepOrphans: in.Forget.KeepOrphanEntities,
			},
		}, nil
	default:
		return arcadedb.MemoryBatchOperation{}, fmt.Errorf("unknown operation type %q", in.Type)
	}
}

func toMemoryBatchFact(
	in MemoryBatchFactInput,
	actor arcadedb.Actor,
	identity string,
	operatorDisplayName string,
) (arcadedb.Fact, error) {
	validFrom, err := parseOptionalTime(in.ValidFrom, "valid_from")
	if err != nil {
		return arcadedb.Fact{}, err
	}
	validTo, err := parseOptionalTime(in.ValidTo, "valid_to")
	if err != nil {
		return arcadedb.Fact{}, err
	}
	return arcadedb.Fact{
		Subject: canonicalSubject(in.Subject, identity, operatorDisplayName), SubjectKind: in.SubjectKind,
		SubjectPole: in.SubjectPole,
		Predicate:   in.Predicate, Object: in.Object, ObjectKind: in.ObjectKind, ObjectPole: in.ObjectPole,
		Statement: in.Statement,
		ValidFrom: validFrom, ValidTo: validTo, TargetFactKey: strings.TrimSpace(in.SupersedesFactKey),
		Source: arcadedb.FactSource{RunID: actor.RunID, WriterRole: actor.Role, MemoryIDs: in.Source.MemoryIDs},
	}, nil
}

func boundMemoryBatchSchema(schema *jsonschema.Schema) {
	boundSchemaString(schema.Properties["idempotency_key"], 1, memoryBatchIdempotencyMaxRunes)
	operations := schema.Properties["operations"]
	operations.MinItems = new(1)
	operations.MaxItems = new(memoryBatchMaxOperations)
	operation := operations.Items
	operation.Properties["type"].Enum = []any{
		string(arcadedb.MemoryBatchUpsertFact), string(arcadedb.MemoryBatchSupersedeFact),
		string(arcadedb.MemoryBatchMergeEntities), string(arcadedb.MemoryBatchForget),
	}
	fact := operation.Properties["fact"]
	// The pole fields take the same bound as the kind they refine. The bound is only
	// there to stop unbounded input: the closed set is NOT an enum here on purpose,
	// because a class outside it must land in Other and be reported back rather than
	// be refused by the client before the server ever sees it.
	for _, name := range []string{
		"subject", "subject_kind", "subject_pole", "object", "object_kind", "object_pole",
	} {
		boundSchemaString(fact.Properties[name], 0, memoryBatchEntityMaxRunes)
	}
	boundSchemaString(fact.Properties["predicate"], 0, memoryBatchPredicateMaxRunes)
	boundSchemaString(fact.Properties["statement"], 0, memoryBatchStatementMaxRunes)
	boundSchemaString(fact.Properties["supersedes_fact_key"], 0, memoryBatchMemoryIDMaxRunes)
	memoryIDs := fact.Properties["source"].Properties["memory_ids"]
	memoryIDs.MaxItems = new(memoryBatchMemoryIDsMaxCount)
	boundSchemaString(memoryIDs.Items, 0, memoryBatchMemoryIDMaxRunes)
	merge := operation.Properties["merge"]
	boundSchemaString(merge.Properties["source"], 0, memoryBatchEntityMaxRunes)
	boundSchemaString(merge.Properties["target"], 0, memoryBatchEntityMaxRunes)
	forget := operation.Properties["forget"]
	boundSchemaString(forget.Properties["source_run_id"], 0, memoryBatchIdempotencyMaxRunes)
	for _, name := range []string{"entity", "subject", "object"} {
		boundSchemaString(forget.Properties[name], 0, memoryBatchEntityMaxRunes)
	}
	boundSchemaString(forget.Properties["predicate"], 0, memoryBatchPredicateMaxRunes)
}

func boundSchemaString(schema *jsonschema.Schema, minimum, maximum int) {
	if minimum > 0 {
		schema.MinLength = new(minimum)
	}
	schema.MaxLength = new(maximum)
}
